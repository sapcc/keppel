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
	"encoding/json"
	"errors"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/gopherpolicy"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/keppel/pkg/database"
	"github.com/sapcc/keppel/pkg/openstack"
	"github.com/sapcc/keppel/pkg/orchestrator"
	gorp "gopkg.in/gorp.v2"
)

//KeppelV1 implements the /keppel/v1/ API endpoints.
type KeppelV1 struct {
	db   *gorp.DbMap
	su   *openstack.ServiceUser
	tv   gopherpolicy.Validator
	orch *orchestrator.API
}

//NewKeppelV1 prepares a new KeppelV1 instance.
func NewKeppelV1(db *gorp.DbMap, su *openstack.ServiceUser, orch *orchestrator.API) (*KeppelV1, error) {
	tv := gopherpolicy.TokenValidator{
		IdentityV3: su.IdentityV3,
	}
	policyPath := os.Getenv("KEPPEL_POLICY_PATH")
	if policyPath == "" {
		return nil, errors.New("missing env variable: KEPPEL_POLICY_PATH")
	}
	err := tv.LoadPolicyFile(policyPath)
	if err != nil {
		return nil, err
	}

	return &KeppelV1{
		db:   db,
		su:   su,
		tv:   &tv,
		orch: orch,
	}, nil
}

//Router prepares a http.Handler
func (api *KeppelV1) Router() http.Handler {
	r := mux.NewRouter()

	//NOTE: Keppel account names are severely restricted because Postgres
	//database names are derived from them. Those are, most importantly,
	//case-insensitive and restricted to 64 chars.
	r.Methods("GET").Path("/keppel/v1/accounts").HandlerFunc(api.handleGetAccounts)
	r.Methods("GET").Path("/keppel/v1/accounts/{account:[a-z0-9-]{0,48}}").HandlerFunc(api.handleGetAccount)
	r.Methods("PUT").Path("/keppel/v1/accounts/{account:[a-z0-9-]{0,48}}").HandlerFunc(api.handlePutAccount)

	return r
}

func (api *KeppelV1) checkToken(r *http.Request) *gopherpolicy.Token {
	token := api.tv.CheckToken(r)
	token.Context.Logger = logg.Debug
	token.Context.Request = mux.Vars(r)
	return token
}

func (api *KeppelV1) handleGetAccounts(w http.ResponseWriter, r *http.Request) {
	token := api.checkToken(r)
	if !token.Require(w, "account:list") {
		return
	}

	var accounts []database.Account
	_, err := api.db.Select(&accounts, "SELECT * FROM accounts ORDER BY name")
	if respondwith.ErrorText(w, err) {
		return
	}

	//restrict accounts to those visible in the current scope
	var accountsFiltered []database.Account
	for _, account := range accounts {
		token.Context.Request["account_project_id"] = account.ProjectUUID
		if token.Check("account:show") {
			accountsFiltered = append(accountsFiltered, account)
		}
	}
	//ensure that this serializes as a list, not as null
	if len(accountsFiltered) == 0 {
		accountsFiltered = []database.Account{}
	}

	respondwith.JSON(w, http.StatusOK, map[string]interface{}{"accounts": accountsFiltered})
}

func (api *KeppelV1) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	token := api.checkToken(r)

	//first very permissive check: can this user GET any accounts AT ALL?
	token.Context.Request["account_project_id"] = token.Context.Auth["project_id"]
	if !token.Require(w, "account:show") {
		return
	}

	//get account from DB to find its project ID
	accountName := mux.Vars(r)["account"]
	account, err := api.findAccount(accountName)
	if respondwith.ErrorText(w, err) {
		return
	}

	//perform final authorization with that project ID
	if account != nil {
		token.Context.Request["account_project_id"] = account.ProjectUUID
		if !token.Check("account:show") {
			account = nil
		}
	}

	if account == nil {
		http.Error(w, "no such account", 404)
		return
	}

	respondwith.JSON(w, http.StatusOK, map[string]interface{}{"account": account})
}

func (api *KeppelV1) handlePutAccount(w http.ResponseWriter, r *http.Request) {
	//decode request body
	var req struct {
		Account struct {
			ProjectUUID string `json:"project_id"`
		} `json:"account"`
	}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "request body is not valid JSON: "+err.Error(), 400)
		return
	}
	if req.Account.ProjectUUID == "" {
		http.Error(w, `missing attribute "account.project_id" in request body`, 400)
		return
	}

	//check permission to create account
	token := api.checkToken(r)
	token.Context.Request["account_project_id"] = req.Account.ProjectUUID
	if !token.Require(w, "account:create") {
		return
	}

	//check if account already exists
	accountName := mux.Vars(r)["account"]
	account, err := api.findAccount(accountName)
	if respondwith.ErrorText(w, err) {
		return
	}
	if account != nil && account.ProjectUUID != req.Account.ProjectUUID {
		http.Error(w, `missing attribute "account.project_id" in request body`, http.StatusConflict)
		return
	}

	//create account if required
	if account == nil {
		tx, err := api.db.Begin()
		if respondwith.ErrorText(w, err) {
			return
		}
		defer database.RollbackUnlessCommitted(tx)

		account = &database.Account{
			Name:        accountName,
			ProjectUUID: req.Account.ProjectUUID,
		}
		err = tx.Insert(account)
		if respondwith.ErrorText(w, err) {
			return
		}

		//before committing this, add the required role assignments
		err = api.su.AddLocalRole(req.Account.ProjectUUID, token)
		if respondwith.ErrorText(w, err) {
			return
		}
		err = tx.Commit()
		if respondwith.ErrorText(w, err) {
			return
		}
	}

	//ensure that keppel-registry is running (TODO remove, only used for testing)
	portChan := make(chan uint16, 1)
	api.orch.GetPortRequestChan <- orchestrator.GetPortRequest{
		Account: *account,
		Result:  portChan,
	}
	logg.Info("keppel-registry for account %s is running on port %d", account.Name, <-portChan)

	respondwith.JSON(w, http.StatusOK, map[string]interface{}{"account": account})
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
