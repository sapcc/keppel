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
	"net/http"

	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/keppel/pkg/auth"
	"github.com/sapcc/keppel/pkg/database"
	"github.com/sapcc/keppel/pkg/keppel"
)

func (api *KeppelV1) handleGetAuth(w http.ResponseWriter, r *http.Request) {
	//parse request
	req, err := auth.ParseRequest(
		r.Header.Get("Authorization"),
		r.URL.RawQuery,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	//find account if scope requested
	var account *database.Account
	if req.Scope != nil && req.Scope.ResourceType == "repository" {
		account, err = keppel.State.DB.FindAccount(req.Scope.AccountName())
		if respondwith.ErrorText(w, err) {
			return
		}
		//do not check account == nil here yet to not leak account existence to
		//unauthorized users
	}

	//check user access
	var authz keppel.Authorization
	if account == nil {
		authz, err = keppel.State.AuthDriver.AuthenticateUser(req.UserName, req.Password)
	} else {
		authz, err = keppel.State.AuthDriver.AuthenticateUserInTenant(
			req.UserName, req.Password, account.AuthTenantID)
	}
	if respondWithAuthError(w, err) {
		return
	}

	//check requested scope and actions
	if req.Scope != nil {
		switch req.Scope.ResourceType {
		case "registry":
			req.Scope.Actions = filterRegistryActions(req.Scope.Actions, authz)
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
	if respondwith.ErrorText(w, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, tokenInfo)
}

func filterRegistryActions(actions []string, authz keppel.Authorization) (result []string) {
	//TODO FIXME: This always returns an empty slice in practice, because when a
	//user wants to see the catalog, their token is not scoped to a specific
	//account. Therefore there are no role assignments that
	//`access.CanViewAccounts()` could consider, so it always returns false.
	//
	//We might have to list role assignments for the user, and *generate* new
	//scopes for each account where the user "CanViewAccount".
	for _, action := range actions {
		if action == "*" {
			result = append(result, action)
		}
	}
	return
}

func filterRepoActions(actions []string, authz keppel.Authorization, account database.Account) (result []string) {
	for _, action := range actions {
		if action == "pull" && authz.HasPermission(keppel.CanViewAccount, account.AuthTenantID) {
			result = append(result, action)
		} else if action == "push" && authz.HasPermission(keppel.CanChangeAccount, account.AuthTenantID) {
			result = append(result, action)
		}
	}
	return
}
