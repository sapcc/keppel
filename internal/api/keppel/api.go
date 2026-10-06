// SPDX-FileCopyrightText: 2018-2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppelv1

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"
	"go.xyrillian.de/gg/gsql"
	"go.xyrillian.de/gg/pathrouter"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/processor"
)

// API contains state variables used by the Keppel V1 API implementation.
type API struct {
	cfg        keppel.Configuration
	authDriver keppel.AuthDriver
	fd         keppel.FederationDriver
	sd         keppel.StorageDriver
	icd        keppel.InboundCacheDriver
	db         *gsql.DB
	auditor    audittools.Auditor
	rle        *keppel.RateLimitEngine // may be nil
	// non-pure functions that can be replaced by deterministic doubles for unit tests
	timeNow func() time.Time
}

// NewAPI constructs a new API instance.
func NewAPI(cfg keppel.Configuration, ad keppel.AuthDriver, fd keppel.FederationDriver, sd keppel.StorageDriver, icd keppel.InboundCacheDriver, db *gsql.DB, auditor audittools.Auditor, rle *keppel.RateLimitEngine) *API {
	return &API{cfg, ad, fd, sd, icd, db, auditor, rle, time.Now}
}

// OverrideTimeNow replaces time.Now with a test double.
func (a *API) OverrideTimeNow(timeNow func() time.Time) *API {
	a.timeNow = timeNow
	return a
}

// AddTo implements the api.API interface.
func (a *API) AddTo(c *httpapi.Composer) {
	//NOTE: Keppel account names are severely restricted because we used to
	// derive Postgres database names from them.
	c.AddTryHandler(pathrouter.Element("keppel", pathrouter.Element("v1", pathrouter.Choice(
		pathrouter.Handlers(pathrouter.ByMethod{http.MethodGet: a.handleGetAPIInfo}),
		pathrouter.Element("accounts", pathrouter.Choice(
			pathrouter.Handlers(pathrouter.ByMethod{http.MethodGet: a.handleGetAccounts}),
			pathrouter.Variable("account", pathrouter.Choice(
				pathrouter.Handlers(pathrouter.ByMethod{http.MethodGet: a.withValidAccountName(a.handleGetAccount), http.MethodPut: a.withValidAccountName(a.handlePutAccount), http.MethodDelete: a.withValidAccountName(a.handleDeleteAccount)}),
				pathrouter.Element("sublease", pathrouter.Handlers(pathrouter.ByMethod{http.MethodPost: a.withValidAccountName(a.handlePostAccountSublease)})),
				pathrouter.Element("security_scan_policies", pathrouter.Handlers(pathrouter.ByMethod{http.MethodGet: a.withValidAccountName(a.handleGetSecurityScanPolicies), http.MethodPut: a.withValidAccountName(a.handlePutSecurityScanPolicies)})),
				pathrouter.Element("repositories", pathrouter.Choice(
					pathrouter.Handlers(pathrouter.ByMethod{http.MethodGet: a.withValidAccountName(a.handleGetRepositories)}),
					pathrouter.CatchAllVariable("repo_name", pathrouter.Choice(
						pathrouter.Element("_manifests", pathrouter.Variable("digest", pathrouter.Choice(
							pathrouter.Element("trivy_report", pathrouter.Handlers(pathrouter.ByMethod{http.MethodGet: a.withValidAccountName(a.handleGetTrivyReport)})),
							pathrouter.Handlers(pathrouter.ByMethod{http.MethodDelete: a.withValidAccountName(a.handleDeleteManifest)}),
						))),
						pathrouter.Element("_manifests", pathrouter.Handlers(pathrouter.ByMethod{http.MethodGet: a.withValidAccountName(a.handleGetManifests), http.MethodDelete: a.withValidAccountName(a.handleDeleteRepository)})),
						pathrouter.Element("_tags", pathrouter.Variable("tag_name", pathrouter.Handlers(pathrouter.ByMethod{http.MethodDelete: a.withValidAccountName(a.handleDeleteTag)}))),
					)),
					pathrouter.CatchAllVariable("repo_name", pathrouter.Handlers(pathrouter.ByMethod{http.MethodDelete: a.withValidAccountName(a.handleDeleteRepository)})),
				)),
			)),
		)),
		pathrouter.Element("peers", pathrouter.Handlers(pathrouter.ByMethod{http.MethodGet: a.handleGetPeers})),
		pathrouter.Element("quotas", pathrouter.Variable("auth_tenant_id", pathrouter.Handlers(pathrouter.ByMethod{http.MethodGet: a.handleGetQuotas, http.MethodPut: a.handlePutQuotas}))),
	))))
}

func (a *API) withValidAccountName(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !models.IsAccountName(pathrouter.VariableValue(r, "account")) {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	}
}

func (a *API) processor() *processor.Processor {
	return processor.New(a.cfg, a.db, a.sd, a.icd, a.auditor, a.fd, a.timeNow)
}

func (a *API) handleGetAPIInfo(w http.ResponseWriter, r *http.Request) {
	respondwith.JSON(w, http.StatusOK, struct {
		AuthDriverName string `json:"auth_driver"`
	}{
		AuthDriverName: a.authDriver.PluginTypeID(),
	})
}

func respondWithAuthError(w http.ResponseWriter, err *keppel.RegistryV2Error) bool {
	if err == nil {
		return false
	}
	err.WriteAsTextTo(w)
	w.Write([]byte("\n"))
	return true
}

func authTenantScope(perm keppel.Permission, authTenantID string) auth.ScopeSet {
	return auth.NewScopeSet(auth.Scope{
		ResourceType: "keppel_auth_tenant",
		ResourceName: authTenantID,
		Actions:      []string{string(perm)},
	})
}

func accountScopeFromRequest(r *http.Request, perm keppel.Permission) auth.ScopeSet {
	return auth.NewScopeSet(auth.Scope{
		ResourceType: "keppel_account",
		ResourceName: pathrouter.VariableValue(r, "account"),
		Actions:      []string{string(perm)},
	})
}

func accountScopes(perm keppel.Permission, names ...models.AccountName) auth.ScopeSet {
	scopes := make([]auth.Scope, len(names))
	for idx, name := range names {
		scopes[idx] = auth.Scope{
			ResourceType: "keppel_account",
			ResourceName: string(name),
			Actions:      []string{string(perm)},
		}
	}
	return auth.NewScopeSet(scopes...)
}

func repoScopeFromRequest(r *http.Request, perm keppel.Permission) auth.ScopeSet {
	return auth.NewScopeSet(auth.Scope{
		ResourceType: "repository",
		ResourceName: fmt.Sprintf("%s/%s", pathrouter.VariableValue(r, "account"), pathrouter.VariableValue(r, "repo_name")),
		Actions:      []string{string(perm)},
	})
}

// TODO: remove `w` argument and return errors using respondwith.CustomStatus(), like in findAccountFromRequest()
func (a *API) authenticateRequest(w http.ResponseWriter, r *http.Request, ss auth.ScopeSet) *auth.Authorization {
	ctx := r.Context()
	authz, _, rerr := auth.IncomingRequest{
		HTTPRequest:          r,
		Scopes:               ss,
		CorrectlyReturn403:   true,
		PartialAccessAllowed: r.URL.Path == "/keppel/v1/accounts",
	}.Authorize(ctx, a.cfg, a.authDriver, a.db)
	if rerr != nil {
		rerr.WriteAsTextTo(w)
		return nil
	}
	return authz
}

var (
	errAccountNotFound   = errors.New("account not found")
	errAccountIsDeleting = errors.New("account is being deleted")
	errRepoNameInvalid   = errors.New("repo name invalid")
	errRepoNotFound      = errors.New("repository not found")
)

// NOTE: The *auth.Authorization argument is only used to ensure that we call authenticateRequest
// first. This is important because this function may otherwise leak information about whether
// accounts exist or not to unauthorized users.
func (a *API) findAccountFromRequest(r *http.Request, _ *auth.Authorization) (models.Account, error) {
	ctx := r.Context()
	accountName := models.AccountName(pathrouter.VariableValue(r, "account"))
	account, err := keppel.FindAccount(ctx, a.db, accountName)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return models.Account{}, respondwith.CustomStatus(http.StatusNotFound, errAccountNotFound)
	case err != nil:
		return models.Account{}, err
	case account.IsDeleting && r.Method == http.MethodGet:
		return models.Account{}, respondwith.CustomStatus(http.StatusConflict, errAccountIsDeleting)
	default:
		return account, nil
	}
}

func (a *API) findReducedAccountFromRequest(r *http.Request, _ *auth.Authorization) (models.ReducedAccount, error) {
	ctx := r.Context()
	accountName := models.AccountName(pathrouter.VariableValue(r, "account"))
	account, err := keppel.FindReducedAccount(ctx, a.db, accountName)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return models.ReducedAccount{}, respondwith.CustomStatus(http.StatusNotFound, errAccountNotFound)
	case err != nil:
		return models.ReducedAccount{}, err
	case account.IsDeleting && r.Method == http.MethodGet:
		return models.ReducedAccount{}, respondwith.CustomStatus(http.StatusConflict, errAccountIsDeleting)
	default:
		return account, nil
	}
}

func (a *API) findRepositoryFromRequest(r *http.Request, accountName models.AccountName) (models.Repository, error) {
	ctx := r.Context()
	repoName := pathrouter.VariableValue(r, "repo_name")
	if !isValidRepoName(repoName) {
		return models.Repository{}, respondwith.CustomStatus(http.StatusUnprocessableEntity, errRepoNameInvalid)
	}

	repo, err := keppel.FindRepository(ctx, a.db, repoName, accountName)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return models.Repository{}, respondwith.CustomStatus(http.StatusNotFound, errRepoNotFound)
	case err != nil:
		return models.Repository{}, err
	default:
		return repo, nil
	}
}

func decodeJSONRequestBody(body io.Reader, target any) error {
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&target)
	if err != nil {
		err = fmt.Errorf("request body is not valid JSON: %w", err)
		return respondwith.CustomStatus(http.StatusBadRequest, err)
	}
	return nil
}

func isValidRepoName(name string) bool {
	if name == "" {
		return false
	}
	for pathComponent := range strings.SplitSeq(name, `/`) {
		if !models.RepoPathComponentRx.MatchString(pathComponent) {
			return false
		}
	}
	return true
}

type paginatedQuery struct {
	SQL         string
	MarkerField string
	Options     url.Values
	BindValues  []any
}

// Prepare assembles a SQL query for pagination with limit.
func (q paginatedQuery) Prepare() (modifiedSQLQuery string, modifiedBindValues []any, limit uint64, err error) {
	// hidden feature: allow lowering the default limit with ?limit= (we only
	// really use this for the unit tests)
	limit = uint64(1000)
	if limitStr := q.Options.Get("limit"); limitStr != "" {
		limitVal, err := strconv.ParseUint(limitStr, 10, 64)
		if err != nil {
			return "", nil, 0, err
		}
		if limitVal < limit { // never allow more than 1000 results at once
			limit = limitVal
		}
	}
	// fetch one more than `limit`: otherwise we cannot distinguish between a
	// truncated 1000-row result and a non-truncated 1000-row result
	query := strings.Replace(q.SQL, `$LIMIT`, strconv.FormatUint(limit+1, 10), 1)

	marker := q.Options.Get("marker")
	if marker == "" {
		query = strings.Replace(query, `$CONDITION`, `TRUE`, 1)
		return query, q.BindValues, limit, nil
	}
	query = strings.Replace(query, `$CONDITION`, q.MarkerField+` > $2`, 1)
	return query, append(q.BindValues, marker), limit, nil
}
