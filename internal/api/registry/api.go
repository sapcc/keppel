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
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/errext"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/processor"
)

// API contains state variables used by the Auth API endpoint.
type API struct {
	cfg     keppel.Configuration
	ad      keppel.AuthDriver
	fd      keppel.FederationDriver
	sd      keppel.StorageDriver
	icd     keppel.InboundCacheDriver
	db      *keppel.DB
	auditor audittools.Auditor
	rle     *keppel.RateLimitEngine // may be nil
	// non-pure functions that can be replaced by deterministic doubles for unit tests
	timeNow           func() time.Time
	generateStorageID func() string
}

// NewAPI constructs a new API instance.
func NewAPI(cfg keppel.Configuration, ad keppel.AuthDriver, fd keppel.FederationDriver, sd keppel.StorageDriver, icd keppel.InboundCacheDriver, db *keppel.DB, auditor audittools.Auditor, rle *keppel.RateLimitEngine) *API {
	return &API{cfg, ad, fd, sd, icd, db, auditor, rle, time.Now, keppel.GenerateStorageID}
}

// OverrideTimeNow replaces time.Now with a test double.
func (a *API) OverrideTimeNow(timeNow func() time.Time) *API {
	a.timeNow = timeNow
	return a
}

// OverrideGenerateStorageID replaces keppel.GenerateStorageID with a test double.
func (a *API) OverrideGenerateStorageID(generateStorageID func() string) *API {
	a.generateStorageID = generateStorageID
	return a
}

// AddTo implements the api.API interface.
func (a *API) AddTo(r *mux.Router) {
	r.Methods("GET").Path("/v2/").HandlerFunc(a.handleToplevel)
	r.Methods("GET").Path("/v2/_catalog").HandlerFunc(a.handleGetCatalog)

	//NOTE: We used to match account name and repository name separately here,
	// but that is not possible anymore since domain-remapped APIs do not have the
	// account name in the URL path. The "repository" variable is split later in
	// checkAccountAccess().
	r.Methods("DELETE").
		Path("/v2/{repository:.+}/blobs/{digest}").
		HandlerFunc(a.handleDeleteBlob)
	r.Methods("GET", "HEAD").
		Path("/v2/{repository:.+}/blobs/{digest}").
		HandlerFunc(a.handleGetOrHeadBlob)
	r.Methods("POST").
		Path("/v2/{repository:.+}/blobs/uploads/").
		HandlerFunc(a.handleStartBlobUpload)
	r.Methods("DELETE").
		Path("/v2/{repository:.+}/blobs/uploads/{uuid}").
		HandlerFunc(a.handleDeleteBlobUpload)
	r.Methods("GET").
		Path("/v2/{repository:.+}/blobs/uploads/{uuid}").
		HandlerFunc(a.handleGetBlobUpload)
	r.Methods("PATCH").
		Path("/v2/{repository:.+}/blobs/uploads/{uuid}").
		HandlerFunc(a.handleContinueBlobUpload)
	r.Methods("PUT").
		Path("/v2/{repository:.+}/blobs/uploads/{uuid}").
		HandlerFunc(a.handleFinishBlobUpload)
	r.Methods("DELETE").
		Path("/v2/{repository:.+}/manifests/{reference}").
		HandlerFunc(a.handleDeleteManifest)
	r.Methods("GET", "HEAD").
		Path("/v2/{repository:.+}/manifests/{reference}").
		HandlerFunc(a.handleGetOrHeadManifest)
	r.Methods("PUT").
		Path("/v2/{repository:.+}/manifests/{reference}").
		HandlerFunc(a.handlePutManifest)
	r.Methods("GET").
		Path("/v2/{repository:.+}/referrers/{reference}").
		HandlerFunc(a.handleRefererrers)
	r.Methods("GET").
		Path("/v2/{repository:.+}/tags/list").
		HandlerFunc(a.handleListTags)
}

func (a *API) processor() *processor.Processor {
	return processor.New(a.cfg, a.db, a.sd, a.icd, a.auditor, a.fd, a.timeNow).OverrideTimeNow(a.timeNow).OverrideGenerateStorageID(a.generateStorageID)
}

// This implements the GET /v2/ endpoint.
func (a *API) handleToplevel(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/v2/")
	// must be set even for 401 responses!
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	_, rerr := auth.IncomingRequest{
		HTTPRequest:           r,
		AllowsAnycast:         true,
		AllowsDomainRemapping: true,
		// `docker login` will use this endpoint to get an auth challenge, so we
		// cannot allow anonymous login here...
		NoImplicitAnonymous: true,
		// ...but we also cannot require any token scopes because `docker login`
		// will ignore the scope defined in the auth challenge, obtain a token
		// without scopes, and then expect to be able to query this endpoint with
		// it.
		Scopes: auth.NewScopeSet(auth.InfoAPIScope),
	}.Authorize(r.Context(), a.cfg, a.ad, a.db)
	if rerr != nil {
		rerr.WriteAsRegistryV2ResponseTo(w, r)
		return
	}

	// The response is not defined beyond code 200, so reply in the same way as
	//https://registry-1.docker.io/v2/, with an empty JSON object.
	respondwith.JSON(w, http.StatusOK, map[string]any{})
}

// Like respondwith.ErrorText(), but writes a RegistryV2Error instead of plain text.
func respondWithError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	} else if perr, ok := errext.As[processor.UpstreamManifestMissingError](err); ok {
		return respondWithError(w, r, perr.Inner)
	} else if rerr, ok := errext.As[*keppel.RegistryV2Error](err); ok {
		if rerr == nil {
			return false
		}
		rerr.WriteAsRegistryV2ResponseTo(w, r)
		return true
	}

	keppel.ErrUnknown.With(err.Error()).WriteAsRegistryV2ResponseTo(w, r)
	return true
}

type repoAccessStrategy int

const (
	failIfRepoMissing             repoAccessStrategy = 0
	createRepoIfMissing           repoAccessStrategy = 1
	createRepoIfMissingAndReplica repoAccessStrategy = 2
)

type anycastRequestInfo struct {
	AccountName     models.AccountName
	RepoName        string
	PrimaryHostName string // the peer who has this account
}

func (info anycastRequestInfo) AsPrometheusLabels() prometheus.Labels {
	// when counting a pull over the anycast API, we don't know the account's auth
	// tenant (since we're not hosting the account), so we're free to abuse ^W use
	// this field for tracking the fact that we were redirecting an anycast
	// request, and where we redirected it
	return prometheus.Labels{
		"account":        string(info.AccountName),
		"auth_tenant_id": "anycast-" + info.PrimaryHostName,
		"method":         "registry-api",
	}
}

// A one-stop-shop authorization checker for all endpoints that set the mux
// variable "repository". On success, returns the account and repository
// that this request is about.
//
// If the account does not exist locally, but the request is for the anycast API
// and the account exists elsewhere, the `anycastHandler` is invoked if given
// instead of giving a 404 response.
func (a *API) checkAccountAccess(w http.ResponseWriter, r *http.Request, strategy repoAccessStrategy, anycastHandler func(http.ResponseWriter, *http.Request, anycastRequestInfo)) (*models.ReducedAccount, *models.Repository, *auth.Authorization) {
	// must be set even for 401 responses!
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	// check that repo name is wellformed
	scope := auth.Scope{
		ResourceType: "repository",
		ResourceName: mux.Vars(r)["repository"],
	}
	if !models.RepoNameWithLeadingSlashRx.MatchString("/" + scope.ResourceName) {
		keppel.ErrNameInvalid.With("invalid repository name").WriteAsRegistryV2ResponseTo(w, r)
		return nil, nil, nil
	}

	// check authorization before FindReducedAccount(); otherwise we might leak
	// information about account existence to unauthorized users
	switch r.Method {
	case http.MethodDelete:
		scope.Actions = []string{"delete"}
	case http.MethodGet, http.MethodHead:
		scope.Actions = []string{"pull"}
	default:
		scope.Actions = []string{"pull", "push"}
	}
	authz, rerr := auth.IncomingRequest{
		HTTPRequest:           r,
		Scopes:                auth.NewScopeSet(scope),
		AllowsAnycast:         anycastHandler != nil,
		AllowsDomainRemapping: true,
	}.Authorize(r.Context(), a.cfg, a.ad, a.db)
	if rerr != nil {
		rerr.WriteAsRegistryV2ResponseTo(w, r)
		return nil, nil, nil
	}

	// we need to know the account to select the registry instance for this request
	repoScope := scope.ParseRepositoryScope(authz.Audience)
	account, err := keppel.FindReducedAccount(a.db, repoScope.AccountName)
	if respondWithError(w, r, err) {
		return nil, nil, nil
	}
	if account == nil {
		// if this is an anycast request, try forwarding it to the peer that has the primary account with this name
		if anycastHandler != nil && authz.Audience.IsAnycast {
			primaryHostName, err := a.fd.FindPrimaryAccount(r.Context(), repoScope.AccountName)
			switch {
			case err == nil:
				// protect against infinite forwarding loops in case different Keppels have
				// different ideas about who is the primary account
				if forwardedBy := r.URL.Query().Get("X-Keppel-Forwarded-By"); forwardedBy != "" {
					msg := fmt.Sprintf("not forwarding anycast request for account %q to %s because request was already forwarded to us by %s",
						repoScope.AccountName, primaryHostName, forwardedBy)
					keppel.ErrUnknown.With(msg).WriteAsRegistryV2ResponseTo(w, r)
				} else {
					mappedPrimaryHostName := authz.Audience.MapPeerHostname(primaryHostName)
					anycastHandler(w, r, anycastRequestInfo{repoScope.AccountName, repoScope.RepositoryName, mappedPrimaryHostName})
				}
				return nil, nil, nil
			case errors.Is(err, keppel.ErrNoSuchPrimaryAccount):
				// fall through to the standard 404 handling below
			default:
				respondWithError(w, r, err)
				return nil, nil, nil
			}
		}
		// defense in depth - if the account does not exist and we're not
		// anycasting, there should not be a valid token (the auth endpoint does not
		// issue tokens with scopes for nonexistent accounts)
		keppel.ErrNameUnknown.With("account not found").WriteAsRegistryV2ResponseTo(w, r)
		return nil, nil, nil
	}

	canCreateRepoIfMissing := false
	canFirstPull := false
	switch strategy {
	case createRepoIfMissing:
		canCreateRepoIfMissing = true
	case createRepoIfMissingAndReplica:
		canFirstPull = authz.ScopeSet.Contains(auth.Scope{
			ResourceType: "repository",
			ResourceName: scope.ResourceName,
			Actions:      []string{"anonymous_first_pull"},
		})
		canCreateRepoIfMissing = account.UpstreamPeerHostName != "" || (account.ExternalPeerURL != "" && (authz.UserIdentity.UserType() == keppel.RegularUser || canFirstPull))
	}

	var repo *models.Repository
	if canCreateRepoIfMissing {
		repo, err = keppel.FindOrCreateRepository(a.db, repoScope.RepositoryName, account.Name)
	} else {
		repo, err = keppel.FindRepository(a.db, repoScope.RepositoryName, account.Name)
	}
	if errors.Is(err, sql.ErrNoRows) || repo == nil {
		if canFirstPull {
			keppel.ErrNameUnknown.With("repository does not exist here, and anonymous users may not create new repositories").WriteAsRegistryV2ResponseTo(w, r)
		} else {
			keppel.ErrNameUnknown.With("repository not found").WriteAsRegistryV2ResponseTo(w, r)
		}
		return nil, nil, nil
	} else if respondWithError(w, r, err) {
		return nil, nil, nil
	}

	return account, repo, authz
}

// Returns the repository name as it appears in URL paths for this API.
func getRepoNameForURLPath(repo models.Repository, authz *auth.Authorization) string {
	// on the regular API, the URL path includes the account name
	if authz.Audience.AccountName == "" {
		return repo.FullName()
	}
	// on domain-remapped APIs, the URL path contains only the bare repository name
	return repo.Name
}
