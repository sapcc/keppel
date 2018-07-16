/*******************************************************************************
*
* Copyright 2018 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package api

import (
	"database/sql"
	"net/http"

	"github.com/gophercloud/gophercloud"
	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/keppel/pkg/database"
	gorp "gopkg.in/gorp.v2"
)

//KeppelV1 implements the /keppel/v1/ API endpoints.
type KeppelV1 struct {
	db         *gorp.DbMap
	identityV3 *gophercloud.ServiceClient
}

//NewKeppelV1 prepares a new instance of the KeppelV1 API handler.
func NewKeppelV1(db *gorp.DbMap, identityV3 *gophercloud.ServiceClient) (*KeppelV1, error) {
	k := &KeppelV1{
		db:         db,
		identityV3: identityV3,
	}

	return k, nil
}

//Router prepares a http.Handler
func (api *KeppelV1) Router() http.Handler {
	r := mux.NewRouter()

	//NOTE: Keppel account names appear in Swift container names, so they may not
	//contain any slashes.
	r.Methods("GET").Path("/keppel/v1/accounts").HandlerFunc(api.handleGetAccounts)
	r.Methods("GET").Path("/keppel/v1/accounts/{account:[^/]+}").HandlerFunc(api.handleGetAccount)
	r.Methods("PUT").Path("/keppel/v1/accounts/{account:[^/]+}").HandlerFunc(api.handlePutAccount)

	return r
}

func (api *KeppelV1) handleGetAccounts(w http.ResponseWriter, r *http.Request) {
	//TODO check policy, restrict `accounts` to those visible in the current scope
	var accounts []database.Account
	_, err := api.db.Select(&accounts, "SELECT * FROM accounts ORDER BY name")
	if respondwith.ErrorText(w, err) {
		return
	}

	respondwith.JSON(w, http.StatusOK, accounts)
}

func (api *KeppelV1) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	//TODO check policy
	accountName := mux.Vars(r)["account"]
	account, err := api.findAccount(accountName)
	if respondwith.ErrorText(w, err) {
		return
	}
	if account == nil {
		http.Error(w, "no such account", 404)
		return
	}

	respondwith.JSON(w, http.StatusOK, account)
}

func (api *KeppelV1) handlePutAccount(w http.ResponseWriter, r *http.Request) {
	//TODO
	w.Write([]byte("put account " + mux.Vars(r)["account"]))
}

func (api *KeppelV1) findAccount(name string) (*database.Account, error) {
	var account database.Account
	err := api.db.SelectOne(&account,
		"SELECT * FROM accounts WHERE name = $1", name)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &account, err
}
