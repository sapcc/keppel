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

package keppelV1API

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/keppel/pkg/keppel"
)

//AddTo adds routes for this API to the given router.
func AddTo(r *mux.Router) {
	//NOTE: Keppel account names are severely restricted because Postgres
	//database names are derived from them. Those are, most importantly,
	//case-insensitive and restricted to 64 chars.
	r.Methods("GET").Path("/keppel/v1/accounts").HandlerFunc(handleGetAccounts)
	r.Methods("GET").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}").HandlerFunc(handleGetAccount)
	r.Methods("PUT").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}").HandlerFunc(handlePutAccount)
}

func respondWithAuthError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(*keppel.RegistryV2Error); ok {
		if e == nil { //WTF: no idea why this is not caught above
			return false
		}
		e.WriteAsTextTo(w)
	} else {
		http.Error(w, "unexpected error: "+err.Error(), http.StatusInternalServerError)
	}
	return true
}

func handleGetAccounts(w http.ResponseWriter, r *http.Request) {
	var err error
	authz, err := keppel.State.AuthDriver.AuthenticateUserFromRequest(r)
	if respondWithAuthError(w, err) {
		return
	}

	var accounts []keppel.Account
	_, err = keppel.State.DB.Select(&accounts, "SELECT * FROM accounts ORDER BY name")
	if respondwith.ErrorText(w, err) {
		return
	}

	//restrict accounts to those visible in the current scope
	var accountsFiltered []keppel.Account
	for _, account := range accounts {
		if authz.HasPermission(keppel.CanViewAccount, account.AuthTenantID) {
			accountsFiltered = append(accountsFiltered, account)
		}
	}
	//ensure that this serializes as a list, not as null
	if len(accountsFiltered) == 0 {
		accountsFiltered = []keppel.Account{}
	}

	respondwith.JSON(w, http.StatusOK, map[string]interface{}{"accounts": accountsFiltered})
}

func handleGetAccount(w http.ResponseWriter, r *http.Request) {
	var err error
	authz, err := keppel.State.AuthDriver.AuthenticateUserFromRequest(r)
	if respondWithAuthError(w, err) {
		return
	}

	//get account from DB to find its AuthTenantID
	accountName := mux.Vars(r)["account"]
	account, err := keppel.State.DB.FindAccount(accountName)
	if respondwith.ErrorText(w, err) {
		return
	}

	//perform final authorization with that AuthTenantID
	if account != nil && !authz.HasPermission(keppel.CanViewAccount, account.AuthTenantID) {
		account = nil
	}

	if account == nil {
		http.Error(w, "no such account", 404)
		return
	}

	respondwith.JSON(w, http.StatusOK, map[string]interface{}{"account": account})
}

func handlePutAccount(w http.ResponseWriter, r *http.Request) {
	//decode request body
	var req struct {
		Account struct {
			AuthTenantID string `json:"auth_tenant_id"`
		} `json:"account"`
	}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "request body is not valid JSON: "+err.Error(), 400)
		return
	}
	if err := keppel.State.AuthDriver.ValidateTenantID(req.Account.AuthTenantID); err != nil {
		http.Error(w, `malformed attribute "account.auth_tenant_id" in request body: `+err.Error(), 400)
		return
	}

	//reserve identifiers for internal pseudo-accounts
	accountName := mux.Vars(r)["account"]
	if strings.HasPrefix(accountName, "keppel-") {
		http.Error(w, `account names with the prefix "keppel-" are reserved for internal use`, 400)
		return
	}

	accountToCreate := keppel.Account{
		Name:         accountName,
		AuthTenantID: req.Account.AuthTenantID,
	}

	//check permission to create account
	authz, err := keppel.State.AuthDriver.AuthenticateUserFromRequest(r)
	if respondWithAuthError(w, err) {
		return
	}
	if !authz.HasPermission(keppel.CanChangeAccount, accountToCreate.AuthTenantID) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	//check if account already exists
	account, err := keppel.State.DB.FindAccount(accountName)
	if respondwith.ErrorText(w, err) {
		return
	}
	if account != nil && account.AuthTenantID != req.Account.AuthTenantID {
		http.Error(w, `account name already in use by a different tenant`, http.StatusConflict)
		return
	}

	//create account if required
	if account == nil {
		tx, err := keppel.State.DB.Begin()
		if respondwith.ErrorText(w, err) {
			return
		}
		defer keppel.RollbackUnlessCommitted(tx)

		account = &accountToCreate
		err = tx.Insert(account)
		if respondwith.ErrorText(w, err) {
			return
		}

		//before committing this, add the required role assignments
		err = keppel.State.AuthDriver.SetupAccount(*account, authz)
		if respondwith.ErrorText(w, err) {
			return
		}
		err = tx.Commit()
		if respondwith.ErrorText(w, err) {
			return
		}
	}

	respondwith.JSON(w, http.StatusOK, map[string]interface{}{"account": account})
}
