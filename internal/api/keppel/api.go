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
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/keppel/internal/keppel"
)

//API contains state variables used by the Keppel V1 API implementation.
type API struct {
	cfg        keppel.Configuration
	authDriver keppel.AuthDriver
	ncDriver   keppel.NameClaimDriver
	sd         keppel.StorageDriver
	db         *keppel.DB
	auditor    keppel.Auditor
}

//NewAPI constructs a new API instance.
func NewAPI(cfg keppel.Configuration, ad keppel.AuthDriver, ncd keppel.NameClaimDriver, sd keppel.StorageDriver, db *keppel.DB, auditor keppel.Auditor) *API {
	return &API{cfg, ad, ncd, sd, db, auditor}
}

//AddTo implements the api.API interface.
func (a *API) AddTo(r *mux.Router) {
	//NOTE: Keppel account names are severely restricted because Postgres
	//database names are derived from them. Those are, most importantly,
	//case-insensitive and restricted to 64 chars.
	r.Methods("GET").Path("/keppel/v1/accounts").HandlerFunc(a.handleGetAccounts)
	r.Methods("GET").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}").HandlerFunc(a.handleGetAccount)
	r.Methods("PUT").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}").HandlerFunc(a.handlePutAccount)

	r.Methods("GET").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}/repositories/{repo_name:.+}/_manifests").HandlerFunc(a.handleGetManifests)
	r.Methods("DELETE").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}/repositories/{repo_name:.+}/_manifests/{digest}").HandlerFunc(a.handleDeleteManifest)

	r.Methods("GET").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}/repositories").HandlerFunc(a.handleGetRepositories)
	r.Methods("DELETE").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}/repositories/{repo_name:.+}").HandlerFunc(a.handleDeleteRepository)

	r.Methods("GET").Path("/keppel/v1/peers").HandlerFunc(a.handleGetPeers)

	r.Methods("GET").Path("/keppel/v1/quotas/{auth_tenant_id}").HandlerFunc(a.handleGetQuotas)
	r.Methods("PUT").Path("/keppel/v1/quotas/{auth_tenant_id}").HandlerFunc(a.handlePutQuotas)
}

func respondWithAuthError(w http.ResponseWriter, err *keppel.RegistryV2Error) bool {
	if err == nil {
		return false
	}
	err.WriteAsTextTo(w)
	w.Write([]byte("\n"))
	return true
}

func (a *API) authenticateAccountScopedRequest(w http.ResponseWriter, r *http.Request, perm keppel.Permission) (*keppel.Account, keppel.Authorization) {
	authz, authErr := a.authDriver.AuthenticateUserFromRequest(r)
	if respondWithAuthError(w, authErr) {
		return nil, nil
	}

	//get account from DB to find its AuthTenantID
	accountName := mux.Vars(r)["account"]
	account, err := a.db.FindAccount(accountName)
	if respondwith.ErrorText(w, err) {
		return nil, nil
	}

	//perform final authorization with that AuthTenantID
	if account != nil && !authz.HasPermission(keppel.CanViewAccount, account.AuthTenantID) {
		account = nil
	}

	//this returns 404 even if the real reason is lack of authorization in order
	//to not leak information about which accounts exist for other tenants
	if account == nil {
		http.Error(w, "no such account", 404)
		return nil, nil
	}

	//enforce permissions other than CanViewAccount if requested
	if !authz.HasPermission(perm, account.AuthTenantID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil, nil
	}

	return account, authz
}

func (a *API) authenticateAuthTenantScopedRequest(w http.ResponseWriter, r *http.Request, perm keppel.Permission) (authTenantID string, authz keppel.Authorization) {
	authz, authErr := a.authDriver.AuthenticateUserFromRequest(r)
	if respondWithAuthError(w, authErr) {
		return "", nil
	}

	//enforce requested permissions
	authTenantID = mux.Vars(r)["auth_tenant_id"]
	if !authz.HasPermission(perm, authTenantID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return "", nil
	}

	return authTenantID, authz
}

func (a *API) findRepositoryFromRequest(w http.ResponseWriter, r *http.Request, account keppel.Account) *keppel.Repository {
	repoName := mux.Vars(r)["repo_name"]
	if !isValidRepoName(repoName) {
		http.Error(w, "not found", http.StatusNotFound)
		return nil
	}

	repo, err := a.db.FindRepository(repoName, account)
	if err == sql.ErrNoRows {
		http.Error(w, "not found", http.StatusNotFound)
		return nil
	}
	if respondwith.ErrorText(w, err) {
		return nil
	}
	return repo
}

var repoPathComponentRx = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

func isValidRepoName(name string) bool {
	if name == "" {
		return false
	}
	for _, pathComponent := range strings.Split(name, `/`) {
		if !repoPathComponentRx.MatchString(pathComponent) {
			return false
		}
	}
	return true
}

type paginatedQuery struct {
	SQL         string
	MarkerField string
	Options     url.Values
	BindValues  []interface{}
}

func (q paginatedQuery) Prepare() (modifiedSQLQuery string, modifiedBindValues []interface{}, limit uint64, err error) {
	//hidden feature: allow lowering the default limit with ?limit= (we only
	//really use this for the unit tests)
	limit = uint64(1000)
	if limitStr := q.Options.Get("limit"); limitStr != "" {
		limitVal, err := strconv.ParseUint(limitStr, 10, 64)
		if err != nil {
			return "", nil, 0, err
		}
		if limitVal < limit { //never allow more than 1000 results at once
			limit = limitVal
		}
	}
	//fetch one more than `limit`: otherwise we cannot distinguish between a
	//truncated 1000-row result and a non-truncated 1000-row result
	query := strings.Replace(q.SQL, `$LIMIT`, strconv.FormatUint(limit+1, 10), 1)

	marker := q.Options.Get("marker")
	if marker == "" {
		query = strings.Replace(query, `$CONDITION`, `TRUE`, 1)
		return query, q.BindValues, limit, nil
	}
	query = strings.Replace(query, `$CONDITION`, q.MarkerField+` > $2`, 1)
	return query, append(q.BindValues, marker), limit, nil
}
