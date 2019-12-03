/******************************************************************************
*
*  Copyright 2019 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package registryv2

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
)

//API contains state variables used by the Auth API endpoint.
type API struct {
	cfg                 keppel.Configuration
	orchestrationDriver keppel.OrchestrationDriver
	db                  *keppel.DB
}

//NewAPI constructs a new API instance.
func NewAPI(cfg keppel.Configuration, od keppel.OrchestrationDriver, db *keppel.DB) *API {
	return &API{cfg, od, db}
}

//AddTo adds routes for this API to the given router.
func (a *API) AddTo(r *mux.Router) {
	r.Methods("GET").Path("/v2/").HandlerFunc(a.handleProxyToplevel)
	r.Methods("GET").Path("/v2/_catalog").HandlerFunc(a.handleProxyCatalog)
	//see internal/api/keppel/accounts.go for why account name format is limited
	rr := r.PathPrefix("/v2/{account:[a-z0-9-]{1,48}}/").Subrouter()

	rr.Methods("DELETE", "GET", "HEAD").
		Path("/{repository:.+}/blobs/{digest}").
		HandlerFunc(a.handleProxyToAccount)
	rr.Methods("POST").
		Path("/{repository:.+}/blobs/uploads/").
		HandlerFunc(a.handleProxyToAccount)
	rr.Methods("DELETE", "GET", "PATCH", "PUT").
		Path("/{repository:.+}/blobs/uploads/{uuid}").
		HandlerFunc(a.handleProxyToAccount)
	rr.Methods("DELETE", "GET", "HEAD", "PUT").
		Path("/{repository:.+}/manifests/{reference}").
		HandlerFunc(a.handleManifest)
	rr.Methods("GET").
		Path("/{repository:.+}/tags/list").
		HandlerFunc(a.handleProxyToAccount)
}

func (a *API) requireBearerToken(w http.ResponseWriter, r *http.Request, scope *auth.Scope) *auth.Token {
	token, err := auth.ParseTokenFromRequest(r, a.cfg)
	if err == nil && scope != nil && !token.Contains(*scope) {
		err = keppel.ErrDenied.With("token does not cover scope %s", scope.String())
	}
	if err != nil {
		logg.Debug("GET %s: %s", r.URL.Path, err.Error())
		challenge := auth.Challenge{Scope: scope}
		if token != nil {
			challenge.Error = "insufficient_scope"
		}
		challenge.WriteTo(w.Header(), a.cfg)
		err.WriteAsRegistryV2ResponseTo(w)
		return nil
	}
	return token
}

//A one-stop-shop authorization checker for all endpoints that set the mux vars
//"account" and "repository". On success, returns the account and repository
//that this request is about.
func (a *API) checkAccountAccess(w http.ResponseWriter, r *http.Request) (account *keppel.Account, repoName string) {
	//must be set even for 401 responses!
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	//check authorization before FindAccount(); otherwise we might leak
	//information about account existence to unauthorized users
	vars := mux.Vars(r)
	scope := auth.Scope{
		ResourceType: "repository",
		ResourceName: fmt.Sprintf("%s/%s", vars["account"], vars["repository"]),
		Actions:      []string{"pull", "push"},
	}
	if r.Method == "GET" || r.Method == "HEAD" {
		scope.Actions = []string{"pull"}
	}
	token := a.requireBearerToken(w, r, &scope)
	if token == nil {
		return nil, ""
	}

	//we need to know the account to select the registry instance for this request
	account, err := a.db.FindAccount(mux.Vars(r)["account"])
	if respondwith.ErrorText(w, err) {
		return nil, ""
	}
	if account == nil {
		//defense in depth - if the account does not exist, there should not be a
		//valid token (the auth endpoint does not issue tokens with scopes for
		//nonexistent accounts)
		keppel.ErrNameUnknown.With("account not found").WriteAsRegistryV2ResponseTo(w)
		return nil, ""
	}

	return account, vars["repository"]
}

type interceptedResponse struct {
	Resp http.Response //with Resp.Body == nil
	Body []byte
}

func (a *API) interceptRequestBody(w http.ResponseWriter, r *http.Request) (buf []byte, ok bool) {
	if r.Body == nil {
		return nil, true
	}

	buf, err := ioutil.ReadAll(r.Body)
	if respondwith.ErrorText(w, err) {
		return nil, false
	}
	err = r.Body.Close()
	if respondwith.ErrorText(w, err) {
		return nil, false
	}

	//replace `r.Body` with a functionally identical copy, to ensure that a
	//subsequent proxyRequestToRegistry() works as expected
	r.Body = ioutil.NopCloser(bytes.NewReader(buf))

	return buf, true
}

func (a *API) proxyRequestToRegistry(w http.ResponseWriter, r *http.Request, account keppel.Account) *interceptedResponse {
	proxyRequest := *r
	proxyRequest.Close = false
	proxyRequest.RequestURI = ""
	if proxyRequest.RemoteAddr != "" && proxyRequest.Header.Get("X-Forwarded-For") == "" {
		host, _, _ := net.SplitHostPort(proxyRequest.RemoteAddr)
		proxyRequest.Header.Set("X-Forwarded-For", host)
	}

	resp, err := a.orchestrationDriver.DoHTTPRequest(account, &proxyRequest,
		keppel.DoNotFollowRedirects)
	if respondwith.ErrorText(w, err) {
		return nil
	}

	result := interceptedResponse{Resp: *resp}
	result.Body, err = ioutil.ReadAll(resp.Body)
	if err == nil {
		err = resp.Body.Close()
	} else {
		resp.Body.Close()
	}
	if respondwith.ErrorText(w, err) {
		return nil
	}
	result.Resp.Body = nil

	return &result
}

func (a *API) proxyResponseToCaller(w http.ResponseWriter, resp *interceptedResponse) {
	for k, v := range resp.Resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.Resp.StatusCode)
	_, err := w.Write(resp.Body)
	if err != nil {
		logg.Error("error copying proxy response: " + err.Error())
	}
}