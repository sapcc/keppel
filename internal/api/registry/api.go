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
	"database/sql"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/go-bits/sre"
	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/processor"
)

//API contains state variables used by the Auth API endpoint.
type API struct {
	cfg     keppel.Configuration
	ad      keppel.AuthDriver
	fd      keppel.FederationDriver
	sd      keppel.StorageDriver
	db      *keppel.DB
	auditor keppel.Auditor
	rle     *keppel.RateLimitEngine //may be nil
	//non-pure functions that can be replaced by deterministic doubles for unit tests
	timeNow           func() time.Time
	generateStorageID func() string
}

//NewAPI constructs a new API instance.
func NewAPI(cfg keppel.Configuration, ad keppel.AuthDriver, fd keppel.FederationDriver, sd keppel.StorageDriver, db *keppel.DB, auditor keppel.Auditor, rle *keppel.RateLimitEngine) *API {
	return &API{cfg, ad, fd, sd, db, auditor, rle, time.Now, keppel.GenerateStorageID}
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

func (a *API) processor() *processor.Processor {
	return processor.New(a.cfg, a.db, a.sd, a.auditor).OverrideTimeNow(a.timeNow).OverrideGenerateStorageID(a.generateStorageID)
}

//This implements the GET /v2/ endpoint.
func (a *API) handleToplevel(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/v2/")
	//must be set even for 401 responses!
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	if a.requireAuthorization(w, r, nil) == nil {
		return
	}

	//The response is not defined beyond code 200, so reply in the same way as
	//https://registry-1.docker.io/v2/, with an empty JSON object.
	respondwith.JSON(w, http.StatusOK, map[string]interface{}{})
}

//Like respondwith.ErrorText(), but writes a RegistryV2Error instead of plain text.
func respondWithError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch err := err.(type) {
	case nil:
		return false
	case *keppel.RegistryV2Error:
		if err == nil {
			return false
		}
		err.WriteAsRegistryV2ResponseTo(w, r)
		return true
	default:
		keppel.ErrUnknown.With(err.Error()).WriteAsRegistryV2ResponseTo(w, r)
		return true
	}
}

//The "with leading slash" simplifies the regex because we need not write the regex for a path element twice.
var repoNameWithLeadingSlashRx = regexp.MustCompile(`^(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)+$`)

type repoAccessStrategy int

const (
	failIfRepoMissing             repoAccessStrategy = 0
	createRepoIfMissing           repoAccessStrategy = 1
	createRepoIfMissingAndReplica repoAccessStrategy = 2
)

type anycastRequestInfo struct {
	AccountName     string
	RepoName        string
	PrimaryHostName string //the peer who has this account
}

func (info anycastRequestInfo) AsPrometheusLabels() prometheus.Labels {
	//when counting a pull over the anycast API, we don't know the account's auth
	//tenant (since we're not hosting the account), so we're free to abuse ^W use
	//this field for tracking the fact that we were redirecting an anycast
	//request, and where we redirected it
	return prometheus.Labels{
		"account":        info.AccountName,
		"auth_tenant_id": "anycast-" + info.PrimaryHostName,
		"method":         "registry-api",
	}
}

//A one-stop-shop authorization checker for all endpoints that set the mux vars
//"account" and "repository". On success, returns the account and repository
//that this request is about.
//
//If the account does not exist locally, but the request is for the anycast API
//and the account exists elsewhere, the `anycastHandler` is invoked if given
//instead of giving a 404 response.
func (a *API) checkAccountAccess(w http.ResponseWriter, r *http.Request, strategy repoAccessStrategy, anycastHandler func(http.ResponseWriter, *http.Request, anycastRequestInfo)) (*keppel.Account, *keppel.Repository, APIAuthorization) {
	//must be set even for 401 responses!
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	//check that repo name is wellformed
	vars := mux.Vars(r)
	accountName, repoName := vars["account"], vars["repository"]
	if !repoNameWithLeadingSlashRx.MatchString("/" + repoName) {
		keppel.ErrNameInvalid.With("invalid repository name").WriteAsRegistryV2ResponseTo(w, r)
		return nil, nil, nil
	}

	//check authorization before FindAccount(); otherwise we might leak
	//information about account existence to unauthorized users
	scope := auth.Scope{
		ResourceType: "repository",
		ResourceName: fmt.Sprintf("%s/%s", accountName, repoName),
	}
	switch r.Method {
	case "DELETE":
		scope.Actions = []string{"delete"}
	case "GET", "HEAD":
		scope.Actions = []string{"pull"}
	default:
		scope.Actions = []string{"pull", "push"}
	}
	authz := a.requireAuthorization(w, r, &scope)
	if authz == nil {
		return nil, nil, nil
	}

	//we need to know the account to select the registry instance for this request
	account, err := keppel.FindAccount(a.db, accountName)
	if respondWithError(w, r, err) {
		return nil, nil, nil
	}
	if account == nil {
		//if this is an anycast request, try forwarding it to the peer that has the primary account with this name
		if anycastHandler != nil && a.cfg.IsAnycastRequest(r) {
			primaryHostName, err := a.fd.FindPrimaryAccount(accountName)
			switch err {
			case error(nil):
				//protect against infinite forwarding loops in case different Keppels have
				//different ideas about how is the primary account
				if forwardedBy := r.URL.Query().Get("X-Keppel-Forwarded-By"); forwardedBy != "" {
					msg := fmt.Sprintf("not forwarding anycast request for account %q to %s because request was already forwarded to us by %s",
						accountName, primaryHostName, forwardedBy)
					keppel.ErrUnknown.With(msg).WriteAsRegistryV2ResponseTo(w, r)
				} else {
					anycastHandler(w, r, anycastRequestInfo{accountName, repoName, primaryHostName})
				}
				return nil, nil, nil
			case keppel.ErrNoSuchPrimaryAccount:
				//fall through to the standard 404 handling below
			default:
				respondWithError(w, r, err)
				return nil, nil, nil
			}
		}
		//defense in depth - if the account does not exist and we're not
		//anycasting, there should not be a valid token (the auth endpoint does not
		//issue tokens with scopes for nonexistent accounts)
		keppel.ErrNameUnknown.With("account not found").WriteAsRegistryV2ResponseTo(w, r)
		return nil, nil, nil
	}

	canCreateRepoIfMissing := false
	if strategy == createRepoIfMissing {
		canCreateRepoIfMissing = true
	} else if strategy == createRepoIfMissingAndReplica {
		canCreateRepoIfMissing = account.UpstreamPeerHostName != "" || (account.ExternalPeerURL != "" && authz.Authorization().IsRegularUser())
	}

	var repo *keppel.Repository
	if canCreateRepoIfMissing {
		repo, err = keppel.FindOrCreateRepository(a.db, repoName, *account)
	} else {
		repo, err = keppel.FindRepository(a.db, repoName, *account)
	}
	if err == sql.ErrNoRows || repo == nil {
		keppel.ErrNameUnknown.With("repository not found").WriteAsRegistryV2ResponseTo(w, r)
		return nil, nil, nil
	} else if respondWithError(w, r, err) {
		return nil, nil, nil
	}

	return account, repo, authz
}

func (a *API) checkRateLimit(w http.ResponseWriter, r *http.Request, account keppel.Account, authz APIAuthorization, action keppel.RateLimitedAction, amount uint64) bool {
	//rate-limiting is optional
	if a.rle == nil {
		return true
	}

	//cluster-internal traffic is exempt from rate-limits (if the request is
	//caused by a user API request, the rate-limit has been checked already
	//before the cluster-internal request was sent)
	if authz.Authorization().IsReplicationUser() {
		return true
	}

	allowed, result, err := a.rle.RateLimitAllows(account, action, amount)
	if respondWithError(w, r, err) {
		return false
	}
	if !allowed {
		retryAfterStr := strconv.FormatUint(uint64(result.RetryAfter/time.Second), 10)
		respondWithError(w, r, keppel.ErrTooManyRequests.With("").WithHeader("Retry-After", retryAfterStr))
		return false
	}

	return true
}
