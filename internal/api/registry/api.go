// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

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
	pr "go.xyrillian.de/gg/pathrouter"
	"go.xyrillian.de/oblast"

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
	db      *oblast.DB
	auditor audittools.Auditor
	rle     *keppel.RateLimitEngine // may be nil
	// non-pure functions that can be replaced by deterministic doubles for unit tests
	timeNow           func() time.Time
	generateStorageID func() string
}

// NewAPI constructs a new API instance.
func NewAPI(cfg keppel.Configuration, ad keppel.AuthDriver, fd keppel.FederationDriver, sd keppel.StorageDriver, icd keppel.InboundCacheDriver, db *oblast.DB, auditor audittools.Auditor, rle *keppel.RateLimitEngine) *API {
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
	// NOTE 1: This uses gg/pathrouter instead of gorilla/mux for the actual path matching
	//         to improve performance esp. for important endpoints like GetManifest and GetBlob.
	// NOTE 2: Most HEAD handlers are deleted to match the endpoint list from
	//         <https://github.com/opencontainers/distribution-spec/blob/main/spec.md#endpoints>.
	r.PathPrefix("/v2/").Handler(pr.Element("v2", pr.Choice(
		pr.Element("/", pr.Handlers(pr.ByMethod{
			http.MethodGet:  a.handleToplevel,
			http.MethodHead: nil,
		})),
		pr.Element("_catalog", pr.Handlers(pr.ByMethod{
			http.MethodGet:  a.handleGetCatalog,
			http.MethodHead: nil,
		})),
		pr.CatchAllVariable("repository", pr.Choice(
			pr.Element("blobs", pr.Choice(
				pr.Variable("digest", pr.Handlers(pr.ByMethod{
					http.MethodDelete: a.handleDeleteBlob,
					http.MethodGet:    a.handleGetOrHeadBlob,
				})),
				pr.Element("uploads", pr.Choice(
					pr.Element("/", pr.Handlers(pr.ByMethod{
						http.MethodPost: a.handleStartBlobUpload,
					})),
					pr.Variable("uuid", pr.Handlers(pr.ByMethod{
						http.MethodDelete: a.handleDeleteBlobUpload,
						http.MethodGet:    a.handleGetBlobUpload,
						http.MethodHead:   nil,
						http.MethodPatch:  a.handleContinueBlobUpload,
						http.MethodPut:    a.handleFinishBlobUpload,
					})),
				)),
			)),
			pr.Element("manifests", pr.Variable("reference", pr.Handlers(pr.ByMethod{
				http.MethodDelete: a.handleDeleteManifest,
				http.MethodGet:    a.handleGetOrHeadManifest,
				http.MethodPut:    a.handlePutManifest,
			}))),
			pr.Element("referrers", pr.Variable("reference", pr.Handlers(pr.ByMethod{
				http.MethodGet:  a.handleGetReferrers,
				http.MethodHead: nil,
			}))),
			pr.Element("tags", pr.Element("list", pr.Handlers(pr.ByMethod{
				http.MethodGet:  a.handleListTags,
				http.MethodHead: nil,
			}))),
		)),
	)))
}

func (a *API) processor() *processor.Processor {
	return processor.New(a.cfg, a.db, a.sd, a.icd, a.auditor, a.fd, a.timeNow).OverrideTimeNow(a.timeNow).OverrideGenerateStorageID(a.generateStorageID)
}

// This implements the GET /v2/ endpoint.
func (a *API) handleToplevel(w http.ResponseWriter, r *http.Request, vars map[string]string) {
	_ = vars
	httpapi.IdentifyEndpoint(r, "/v2/")
	ctx := r.Context()

	// must be set even for 401 responses!
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	_, _, rerr := auth.IncomingRequest{
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
	}.Authorize(ctx, a.cfg, a.ad, a.db)
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

	// TODO: should use obfuscation (like respondwith.ObfuscatedErrorText())
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

func (info anycastRequestInfo) asPrometheusLabels() prometheus.Labels {
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
//
// TODO: remove `w` argument and return errors using respondwith.CustomStatus(), like in findAccountFromRequest()
// TODO: return non-pointer arguments to avoid useless heap allocations
func (a *API) checkAccountAccess(w http.ResponseWriter, r *http.Request, vars map[string]string, strategy repoAccessStrategy, anycastHandler func(http.ResponseWriter, *http.Request, map[string]string, anycastRequestInfo)) (*models.ReducedAccount, *models.ReducedRepository, *auth.Authorization, *auth.Challenge) {
	ctx := r.Context()

	// must be set even for 401 responses!
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	// check that repo name is wellformed
	scope := auth.Scope{
		ResourceType: "repository",
		ResourceName: vars["repository"],
	}
	if !models.RepoNameWithLeadingSlashRx.MatchString("/" + scope.ResourceName) {
		keppel.ErrNameInvalid.With("invalid repository name").WriteAsRegistryV2ResponseTo(w, r)
		return nil, nil, nil, nil
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
	authz, challenge, rerr := auth.IncomingRequest{
		HTTPRequest:           r,
		Scopes:                auth.NewScopeSet(scope),
		AllowsAnycast:         anycastHandler != nil,
		AllowsDomainRemapping: true,
	}.Authorize(ctx, a.cfg, a.ad, a.db)
	if rerr != nil {
		rerr.WriteAsRegistryV2ResponseTo(w, r)
		return nil, nil, nil, nil
	}

	// we need to know the account to select the registry instance for this request
	repoScope := scope.ParseRepositoryScope(authz.Audience)
	account, err := keppel.FindReducedAccount(ctx, a.db, repoScope.AccountName)
	if errors.Is(err, sql.ErrNoRows) {
		// if this is an anycast request, try forwarding it to the peer that has the primary account with this name
		if anycastHandler != nil && authz.Audience.IsAnycast {
			primaryHostName, err := a.fd.FindPrimaryAccount(ctx, repoScope.AccountName)
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
					anycastHandler(w, r, vars, anycastRequestInfo{repoScope.AccountName, repoScope.RepositoryName, mappedPrimaryHostName})
				}
				return nil, nil, nil, nil
			case errors.Is(err, keppel.ErrNoSuchPrimaryAccount):
				// fall through to the standard 404 handling below
			default:
				respondWithError(w, r, err)
				return nil, nil, nil, nil
			}
		}
		// defense in depth - if the account does not exist and we're not
		// anycasting, there should not be a valid token (the auth endpoint does not
		// issue tokens with scopes for nonexistent accounts)
		keppel.ErrNameUnknown.With("account not found").WriteAsRegistryV2ResponseTo(w, r)
		return nil, nil, nil, nil
	}
	if respondWithError(w, r, err) {
		return nil, nil, nil, nil
	}

	canCreateRepoIfMissing := false
	switch strategy {
	case createRepoIfMissing:
		canCreateRepoIfMissing = true
	case createRepoIfMissingAndReplica:
		canCreateRepoIfMissing = account.UpstreamPeerHostName != "" || (account.ExternalPeerURL != "" && (authz.UserIdentity.UserType() == keppel.RegularUser || authz.ScopeSet.AllowsAnonymousFirstPullOn(scope.ResourceName)))
	}

	var repo models.ReducedRepository
	if canCreateRepoIfMissing {
		repo, err = keppel.FindOrCreateReducedRepository(ctx, a.db, repoScope.RepositoryName, account.Name)
	} else {
		repo, err = keppel.FindReducedRepository(ctx, a.db, repoScope.RepositoryName, account.Name)
	}
	if errors.Is(err, sql.ErrNoRows) {
		if strategy == createRepoIfMissingAndReplica && account.ExternalPeerURL != "" && authz.UserIdentity.UserType() == keppel.AnonymousUser {
			rerr := keppel.ErrDenied.With("repository does not exist here, and anonymous users may not create new repositories")
			// this must be a 401 and include a challenge; clients should be able to understand that
			// they can retry this after authenticating and expect a different result
			challenge.AddTo(rerr).WithStatus(http.StatusUnauthorized).WriteAsRegistryV2ResponseTo(w, r)
		} else {
			keppel.ErrNameUnknown.With("repository not found").WriteAsRegistryV2ResponseTo(w, r)
		}
		return nil, nil, nil, nil
	} else if respondWithError(w, r, err) {
		return nil, nil, nil, nil
	}

	return &account, &repo, authz, challenge
}

// Returns the repository name as it appears in URL paths for this API.
func getRepoNameForURLPath(repo models.ReducedRepository, authz *auth.Authorization) string {
	// on the regular API, the URL path includes the account name
	if authz.Audience.AccountName == "" {
		return repo.FullName()
	}
	// on domain-remapped APIs, the URL path contains only the bare repository name
	return repo.Name
}
