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
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/go-bits/sre"
	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
)

//API contains state variables used by the Auth API endpoint.
type API struct {
	cfg keppel.Configuration
	sd  keppel.StorageDriver
	db  *keppel.DB
	//non-pure functions that can be replaced by deterministic doubles for unit tests
	timeNow           func() time.Time
	generateStorageID func() string
}

//NewAPI constructs a new API instance.
func NewAPI(cfg keppel.Configuration, sd keppel.StorageDriver, db *keppel.DB) *API {
	return &API{cfg, sd, db, time.Now, keppel.GenerateStorageID}
}

//OverrideTimeNow replaces time.Now with a test double.
func (a *API) OverrideTimeNow(timeNow func() time.Time) *API {
	a.timeNow = timeNow
	return a
}

//OverrideGenerateStorageID replaces keppel.GenerateStorageID with a test double.
func (a *API) OverrideGenerateStorageID(generateStorageID func() string) *API {
	a.generateStorageID = generateStorageID
	return a
}

//AddTo implements the api.API interface.
func (a *API) AddTo(r *mux.Router) {
	r.Methods("GET").Path("/v2/").HandlerFunc(a.handleToplevel)
	r.Methods("GET").Path("/v2/_catalog").HandlerFunc(a.handleGetCatalog)
	//see internal/api/keppel/accounts.go for why account name format is limited
	rr := r.PathPrefix("/v2/{account:[a-z0-9-]{1,48}}/").Subrouter()

	rr.Methods("DELETE").
		Path("/{repository:.+}/blobs/{digest}").
		HandlerFunc(a.handleDeleteBlob)
	rr.Methods("GET", "HEAD").
		Path("/{repository:.+}/blobs/{digest}").
		HandlerFunc(a.handleGetOrHeadBlob)
	rr.Methods("POST").
		Path("/{repository:.+}/blobs/uploads/").
		HandlerFunc(a.handleStartBlobUpload)
	rr.Methods("DELETE").
		Path("/{repository:.+}/blobs/uploads/{uuid}").
		HandlerFunc(a.handleDeleteBlobUpload)
	rr.Methods("GET").
		Path("/{repository:.+}/blobs/uploads/{uuid}").
		HandlerFunc(a.handleGetBlobUpload)
	rr.Methods("PATCH").
		Path("/{repository:.+}/blobs/uploads/{uuid}").
		HandlerFunc(a.handleContinueBlobUpload)
	rr.Methods("PUT").
		Path("/{repository:.+}/blobs/uploads/{uuid}").
		HandlerFunc(a.handleFinishBlobUpload)
	rr.Methods("DELETE").
		Path("/{repository:.+}/manifests/{reference}").
		HandlerFunc(a.handleDeleteManifest)
	rr.Methods("GET", "HEAD").
		Path("/{repository:.+}/manifests/{reference}").
		HandlerFunc(a.handleGetOrHeadManifest)
	rr.Methods("PUT").
		Path("/{repository:.+}/manifests/{reference}").
		HandlerFunc(a.handlePutManifest)
	rr.Methods("GET").
		Path("/{repository:.+}/tags/list").
		HandlerFunc(a.handleListTags)
}

//This implements the GET /v2/ endpoint.
func (a *API) handleToplevel(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/v2/")
	//must be set even for 401 responses!
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	if a.requireBearerToken(w, r, nil) == nil {
		return
	}

	//The response is not defined beyond code 200, so reply in the same way as
	//https://registry-1.docker.io/v2/, with an empty JSON object.
	respondwith.JSON(w, http.StatusOK, map[string]interface{}{})
}

//Like respondwith.ErrorText(), but writes a RegistryV2Error instead of plain text.
func respondWithError(w http.ResponseWriter, err error) bool {
	switch err := err.(type) {
	case nil:
		return false
	case *keppel.RegistryV2Error:
		if err == nil {
			return false
		}
		err.WriteAsRegistryV2ResponseTo(w)
		return true
	default:
		keppel.ErrUnknown.With(err.Error()).WriteAsRegistryV2ResponseTo(w)
		return true
	}
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

//The "with leading slash" simplifies the regex because we need not write the regex for a path element twice.
var repoNameWithLeadingSlashRx = regexp.MustCompile(`^(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)+$`)

//A one-stop-shop authorization checker for all endpoints that set the mux vars
//"account" and "repository". On success, returns the account and repository
//that this request is about.
func (a *API) checkAccountAccess(w http.ResponseWriter, r *http.Request) (account *keppel.Account, repoName string, token *auth.Token) {
	//must be set even for 401 responses!
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	//check that repo name is wellformed
	vars := mux.Vars(r)
	repoName = vars["repository"]
	if !repoNameWithLeadingSlashRx.MatchString("/" + repoName) {
		keppel.ErrNameInvalid.With("invalid repository name").WriteAsRegistryV2ResponseTo(w)
		return
	}

	//check authorization before FindAccount(); otherwise we might leak
	//information about account existence to unauthorized users
	scope := auth.Scope{
		ResourceType: "repository",
		ResourceName: fmt.Sprintf("%s/%s", vars["account"], vars["repository"]),
	}
	switch r.Method {
	case "DELETE":
		scope.Actions = []string{"delete"}
	case "GET", "HEAD":
		scope.Actions = []string{"pull"}
	default:
		scope.Actions = []string{"pull", "push"}
	}
	token = a.requireBearerToken(w, r, &scope)
	if token == nil {
		return nil, "", nil
	}

	//we need to know the account to select the registry instance for this request
	account, err := a.db.FindAccount(mux.Vars(r)["account"])
	if respondWithError(w, err) {
		return nil, "", nil
	}
	if account == nil {
		//defense in depth - if the account does not exist, there should not be a
		//valid token (the auth endpoint does not issue tokens with scopes for
		//nonexistent accounts)
		keppel.ErrNameUnknown.With("account not found").WriteAsRegistryV2ResponseTo(w)
		return nil, "", nil
	}

	return account, repoName, token
}
