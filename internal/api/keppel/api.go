/******************************************************************************
*
*  Copyright 2018-2019 SAP SE
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

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/respondwith"

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
	db         *keppel.DB
	auditor    audittools.Auditor
	rle        *keppel.RateLimitEngine // may be nil
	// non-pure functions that can be replaced by deterministic doubles for unit tests
	timeNow func() time.Time
}

// NewAPI constructs a new API instance.
func NewAPI(cfg keppel.Configuration, ad keppel.AuthDriver, fd keppel.FederationDriver, sd keppel.StorageDriver, icd keppel.InboundCacheDriver, db *keppel.DB, auditor audittools.Auditor, rle *keppel.RateLimitEngine) *API {
	return &API{cfg, ad, fd, sd, icd, db, auditor, rle, time.Now}
}

// OverrideTimeNow replaces time.Now with a test double.
func (a *API) OverrideTimeNow(timeNow func() time.Time) *API {
	a.timeNow = timeNow
	return a
}

// AddTo implements the api.API interface.
func (a *API) AddTo(r *mux.Router) {
	r.Methods("GET").Path("/keppel/v1").HandlerFunc(a.handleGetAPIInfo)

	//NOTE: Keppel account names are severely restricted because we used to
	// derive Postgres database names from them.
	r.Methods("GET").Path("/keppel/v1/accounts").HandlerFunc(a.handleGetAccounts)
	r.Methods("GET").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}").HandlerFunc(a.handleGetAccount)
	r.Methods("PUT").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}").HandlerFunc(a.handlePutAccount)
	r.Methods("DELETE").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}").HandlerFunc(a.handleDeleteAccount)
	r.Methods("POST").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}/sublease").HandlerFunc(a.handlePostAccountSublease)
	r.Methods("GET").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}/security_scan_policies").HandlerFunc(a.handleGetSecurityScanPolicies)
	r.Methods("PUT").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}/security_scan_policies").HandlerFunc(a.handlePutSecurityScanPolicies)

	r.Methods("GET").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}/repositories/{repo_name:.+}/_manifests").HandlerFunc(a.handleGetManifests)
	r.Methods("DELETE").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}/repositories/{repo_name:.+}/_manifests/{digest}").HandlerFunc(a.handleDeleteManifest)
	r.Methods("GET").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}/repositories/{repo_name:.+}/_manifests/{digest}/trivy_report").HandlerFunc(a.handleGetTrivyReport)
	r.Methods("DELETE").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}/repositories/{repo_name:.+}/_tags/{tag_name}").HandlerFunc(a.handleDeleteTag)

	r.Methods("GET").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}/repositories").HandlerFunc(a.handleGetRepositories)
	r.Methods("DELETE").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}/repositories/{repo_name:.+}").HandlerFunc(a.handleDeleteRepository)

	r.Methods("GET").Path("/keppel/v1/peers").HandlerFunc(a.handleGetPeers)

	r.Methods("GET").Path("/keppel/v1/quotas/{auth_tenant_id}").HandlerFunc(a.handleGetQuotas)
	r.Methods("PUT").Path("/keppel/v1/quotas/{auth_tenant_id}").HandlerFunc(a.handlePutQuotas)

	// Besides the native Keppel API, this handler also implements LIQUID.
	// Ref: <https://pkg.go.dev/github.com/sapcc/go-api-declarations/liquid>
	r.Methods("GET").Path("/liquid/v1/info").HandlerFunc(a.handleLiquidGetInfo)
	r.Methods("POST").Path("/liquid/v1/report-capacity").HandlerFunc(a.handleLiquidReportCapacity)
	r.Methods("POST").Path("/liquid/v1/projects/{auth_tenant_id}/report-usage").HandlerFunc(a.handleLiquidReportUsage)
	r.Methods("PUT").Path("/liquid/v1/projects/{auth_tenant_id}/quota").HandlerFunc(a.handleLiquidSetQuota)
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
		ResourceName: mux.Vars(r)["account"],
		Actions:      []string{string(perm)},
	})
}

func accountScopes(perm keppel.Permission, accounts ...models.Account) auth.ScopeSet {
	scopes := make([]auth.Scope, len(accounts))
	for idx, account := range accounts {
		scopes[idx] = auth.Scope{
			ResourceType: "keppel_account",
			ResourceName: string(account.Name),
			Actions:      []string{string(perm)},
		}
	}
	return auth.NewScopeSet(scopes...)
}

func repoScopeFromRequest(r *http.Request, perm keppel.Permission) auth.ScopeSet {
	vars := mux.Vars(r)
	return auth.NewScopeSet(auth.Scope{
		ResourceType: "repository",
		ResourceName: fmt.Sprintf("%s/%s", vars["account"], vars["repo_name"]),
		Actions:      []string{string(perm)},
	})
}

func (a *API) authenticateRequest(w http.ResponseWriter, r *http.Request, ss auth.ScopeSet) *auth.Authorization {
	authz, rerr := auth.IncomingRequest{
		HTTPRequest:          r,
		Scopes:               ss,
		CorrectlyReturn403:   true,
		PartialAccessAllowed: r.URL.Path == "/keppel/v1/accounts",
	}.Authorize(r.Context(), a.cfg, a.authDriver, a.db)
	if rerr != nil {
		rerr.WriteAsTextTo(w)
		return nil
	}
	return authz
}

// NOTE: The *auth.Authorization argument is only used to ensure that we call authenticateRequest
// first. This is important because this function may otherwise leak information about whether
// accounts exist or not to unauthorized users.
func (a *API) findAccountFromRequest(w http.ResponseWriter, r *http.Request, _ *auth.Authorization) *models.Account {
	accountName := models.AccountName(mux.Vars(r)["account"])
	account, err := keppel.FindAccount(a.db, accountName)
	if respondwith.ErrorText(w, err) {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "account not found", http.StatusNotFound)
		return nil
	}
	if account.IsDeleting && r.Method == http.MethodGet {
		http.Error(w, "account is being deleted", http.StatusConflict)
		return nil
	}
	return account
}

func (a *API) findRepositoryFromRequest(w http.ResponseWriter, r *http.Request, accountName models.AccountName) *models.Repository {
	repoName := mux.Vars(r)["repo_name"]
	if !isValidRepoName(repoName) {
		http.Error(w, "repo name invalid", http.StatusUnprocessableEntity)
		return nil
	}

	repo, err := keppel.FindRepository(a.db, repoName, accountName)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "repo not found", http.StatusNotFound)
		return nil
	}
	if respondwith.ErrorText(w, err) {
		return nil
	}
	return repo
}

func decodeJSONRequestBody(w http.ResponseWriter, body io.Reader, target any) (ok bool) {
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&target)
	if err != nil {
		http.Error(w, "request body is not valid JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func isValidRepoName(name string) bool {
	if name == "" {
		return false
	}
	for _, pathComponent := range strings.Split(name, `/`) {
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
