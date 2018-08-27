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

package authapi

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/keppel/pkg/auth"
	"github.com/sapcc/keppel/pkg/keppel"
)

//AddTo adds routes for this API to the given router.
func AddTo(r *mux.Router) {
	r.Methods("GET").Path("/keppel/v1/auth").HandlerFunc(handleGetAuth)
}

func handleGetAuth(w http.ResponseWriter, r *http.Request) {
	//parse request
	req, err := auth.ParseRequest(
		r.Header.Get("Authorization"),
		r.URL.RawQuery,
	)
	if err != nil {
		respondwith.JSON(w, http.StatusBadRequest, map[string]string{"details": err.Error()})
		return
	}

	//find account if scope requested
	var account *keppel.Account
	if req.Scope != nil && req.Scope.ResourceType == "repository" {
		account, err = keppel.State.DB.FindAccount(req.Scope.AccountName())
		if err != nil {
			respondwith.JSON(w, http.StatusBadRequest, map[string]string{"details": err.Error()})
			return
		}
		//do not check account == nil here yet to not leak account existence to
		//unauthorized users
	}

	//check user access
	authz, err := keppel.State.AuthDriver.AuthenticateUser(req.UserName, req.Password)
	if err != nil {
		respondwith.JSON(w, http.StatusUnauthorized, map[string]string{"details": err.Error()})
		return
	}

	//check requested scope and actions
	if req.Scope != nil {
		switch req.Scope.ResourceType {
		case "registry":
			if req.Scope.ResourceName == "catalog" {
				req.Scope.Actions = []string{"*"}
				req.CompiledScopes, err = compileCatalogAccess(authz)
				if err != nil {
					respondwith.JSON(w, http.StatusBadRequest, map[string]string{"details": err.Error()})
					return
				}
			} else {
				req.Scope.Actions = nil
			}
		case "repository":
			if account == nil {
				req.Scope.Actions = nil
			} else {
				req.Scope.Actions = filterRepoActions(req.Scope.Actions, authz, *account)
			}
		default:
			req.Scope.Actions = nil
		}
	}

	tokenInfo, err := req.ToToken().ToResponse()
	if err != nil {
		respondwith.JSON(w, http.StatusBadRequest, map[string]string{"details": err.Error()})
		return
	}
	respondwith.JSON(w, http.StatusOK, tokenInfo)
}

func filterRepoActions(actions []string, authz keppel.Authorization, account keppel.Account) (result []string) {
	for _, action := range actions {
		if action == "pull" && authz.HasPermission(keppel.CanViewAccount, account.AuthTenantID) {
			result = append(result, action)
		} else if action == "push" && authz.HasPermission(keppel.CanChangeAccount, account.AuthTenantID) {
			result = append(result, action)
		}
	}
	return
}

func compileCatalogAccess(authz keppel.Authorization) ([]auth.Scope, error) {
	var accounts []keppel.Account
	_, err := keppel.State.DB.Select(&accounts, "SELECT * FROM accounts ORDER BY name")
	if err != nil {
		return nil, err
	}

	var scopes []auth.Scope
	for _, account := range accounts {
		if authz.HasPermission(keppel.CanViewAccount, account.AuthTenantID) {
			scopes = append(scopes, auth.Scope{
				ResourceType: "keppel_account",
				ResourceName: account.Name,
				Actions:      []string{"view"},
			})
		}
	}

	return scopes, nil
}
