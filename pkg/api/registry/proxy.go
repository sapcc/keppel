/******************************************************************************
*
*  Copyright 2018 SAP SE
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

package registryV2API

import (
	"io"
	"net"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/keppel/pkg/auth"
	"github.com/sapcc/keppel/pkg/keppel"
)

//AddTo adds routes for this API to the given router.
func AddTo(r *mux.Router) {
	r.Methods("GET").Path("/v2/").HandlerFunc(handleProxyToplevel)
	r.Methods("GET").Path("/v2/_catalog").HandlerFunc(handleProxyCatalog)
	r.PathPrefix("/v2/{account:[a-z0-9-]{1,48}}/").HandlerFunc(handleProxyToAccount)
	//see pkg/api/keppel/accounts.go for why account name format is limited
}

func requireBearerToken(w http.ResponseWriter, r *http.Request, scope *auth.Scope) *auth.Token {
	token, err := auth.ParseTokenFromRequest(r)
	if err == nil && scope != nil && !token.Contains(*scope) {
		err = keppel.ErrDenied.With("token does not cover scope %s", scope.String())
	}
	if err != nil {
		logg.Info("GET %s: %s", r.URL.Path, err.Error())
		auth.Challenge{Scope: scope}.WriteTo(w.Header())
		err.WriteAsRegistryV2ResponseTo(w)
		return nil
	}
	return token
}

//This implements the GET /v2/ endpoint.
func handleProxyToplevel(w http.ResponseWriter, r *http.Request) {
	//must be set even for 401 responses!
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	if requireBearerToken(w, r, nil) == nil {
		return
	}

	respondwith.JSON(w, http.StatusOK, map[string]interface{}{})
}

//This implements the GET /v2/_catalog endpoint.
func handleProxyCatalog(w http.ResponseWriter, r *http.Request) {
	//must be set even for 401 responses!
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	requiredScope := auth.MustParseScope("registry:catalog:*")
	if requireBearerToken(w, r, &requiredScope) == nil {
		return
	}

	//TODO: stub
	respondwith.JSON(w, http.StatusOK, map[string]interface{}{
		"repositories": []interface{}{},
	})
}

func handleProxyToAccount(w http.ResponseWriter, r *http.Request) {
	accountName := mux.Vars(r)["account"]
	account, err := keppel.State.DB.FindAccount(accountName)
	if respondwith.ErrorText(w, err) {
		return
	}
	if account == nil {
		//TODO respond in the same way as the registry would on Unauthorized, to
		//not leak information about which accounts exist to unauthorized users
		//
		//We might have to do the full auth game right here already before even
		//proxying to keppel-registry, but that would require recognizing all API
		//endpoints.
		keppel.ErrNameUnknown.With("account not found").WriteAsRegistryV2ResponseTo(w)
		return
	}

	proxyRequest := *r
	proxyRequest.Close = false
	proxyRequest.RequestURI = ""
	if proxyRequest.RemoteAddr != "" && proxyRequest.Header.Get("X-Forwarded-For") == "" {
		host, _, _ := net.SplitHostPort(proxyRequest.RemoteAddr)
		proxyRequest.Header.Set("X-Forwarded-For", host)
	}

	resp, err := keppel.State.OrchestrationDriver.DoHTTPRequest(*account, &proxyRequest)
	if respondwith.ErrorText(w, err) {
		return
	}
	defer resp.Body.Close()

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		logg.Error("error copying proxy response: " + err.Error())
	}
}
