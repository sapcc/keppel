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
	"net/http"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/keppel/internal/keppel"
)

//API contains state variables used by the Keppel V1 API implementation.
type API struct {
	authDriver keppel.AuthDriver
	ncDriver   keppel.NameClaimDriver
	db         *keppel.DB
	auditor    keppel.Auditor
}

//NewAPI constructs a new API instance.
func NewAPI(ad keppel.AuthDriver, ncd keppel.NameClaimDriver, db *keppel.DB, auditor keppel.Auditor) *API {
	return &API{ad, ncd, db, auditor}
}

//AddTo adds routes for this API to the given router.
func (a *API) AddTo(r *mux.Router) {
	//NOTE: Keppel account names are severely restricted because Postgres
	//database names are derived from them. Those are, most importantly,
	//case-insensitive and restricted to 64 chars.
	r.Methods("GET").Path("/keppel/v1/accounts").HandlerFunc(a.handleGetAccounts)
	r.Methods("GET").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}").HandlerFunc(a.handleGetAccount)
	r.Methods("PUT").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}").HandlerFunc(a.handlePutAccount)

	r.Methods("GET").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}/repositories").HandlerFunc(a.handleGetRepositories)
	r.Methods("GET").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}/repositories/{repo_name:.+}/_manifests").HandlerFunc(a.handleGetManifests)
}

func respondWithAuthError(w http.ResponseWriter, err *keppel.RegistryV2Error) bool {
	if err == nil {
		return false
	}
	err.WriteAsTextTo(w)
	w.Write([]byte("\n"))
	return true
}

func (a *API) authenticateAccountScopedRequest(w http.ResponseWriter, r *http.Request, perm keppel.Permission) *keppel.Account {
	authz, authErr := a.authDriver.AuthenticateUserFromRequest(r)
	if respondWithAuthError(w, authErr) {
		return nil
	}

	//get account from DB to find its AuthTenantID
	accountName := mux.Vars(r)["account"]
	account, err := a.db.FindAccount(accountName)
	if respondwith.ErrorText(w, err) {
		return nil
	}

	//perform final authorization with that AuthTenantID
	if account != nil && !authz.HasPermission(keppel.CanViewAccount, account.AuthTenantID) {
		account = nil
	}

	//this returns 404 even if the real reason is lack of authorization in order
	//to not leak information about which accounts exist for other tenants
	if account == nil {
		http.Error(w, "no such account", 404)
	}
	return account
}
