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
	"strings"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
)

//API contains state variables used by the Auth API endpoint.
type API struct {
	cfg        keppel.Configuration
	authDriver keppel.AuthDriver
	db         *keppel.DB
}

//NewAPI constructs a new API instance.
func NewAPI(cfg keppel.Configuration, ad keppel.AuthDriver, db *keppel.DB) *API {
	return &API{cfg, ad, db}
}

//AddTo adds routes for this API to the given router.
func (a *API) AddTo(r *mux.Router) {
	r.Methods("GET").Path("/keppel/v1/auth").HandlerFunc(a.handleGetAuth)
}

func respondWithError(w http.ResponseWriter, code int, err error) bool {
	if err != nil {
		respondwith.JSON(w, code, map[string]string{"details": err.Error()})
		return true
	}
	return false
}

func (a *API) handleGetAuth(w http.ResponseWriter, r *http.Request) {
	//parse request
	req, err := auth.ParseRequest(
		r.Header.Get("Authorization"),
		r.URL.RawQuery,
		a.cfg,
	)
	if respondWithError(w, http.StatusBadRequest, err) {
		return
	}

	//find account if scope requested
	var account *keppel.Account
	if req.Scope.ResourceType == "repository" && req.Scope.AccountName() != "" {
		account, err = a.db.FindAccount(req.Scope.AccountName())
		if respondWithError(w, http.StatusInternalServerError, err) {
			return
		}
		//do not check account == nil here yet to not leak account existence to
		//unauthorized users
	}

	//check user access
	authz, rerr := a.authDriver.AuthenticateUser(req.UserName, req.Password)
	if rerr != nil {
		rerr.WriteAsRegistryV2ResponseTo(w)
		return
	}

	//check requested scope and actions
	switch req.Scope.ResourceType {
	case "registry":
		if req.Scope.ResourceName == "catalog" && containsString(req.Scope.Actions, "*") {
			req.Scope.Actions = []string{"*"}
			req.CompiledScopes, err = a.compileCatalogAccess(authz)
			if respondWithError(w, http.StatusInternalServerError, err) {
				return
			}
		} else {
			req.Scope.Actions = nil
		}
	case "repository":
		if account == nil || !strings.Contains(req.Scope.ResourceName, "/") {
			req.Scope.Actions = nil
		} else {
			req.Scope.Actions = filterRepoActions(req.Scope.Actions, authz, *account)
		}
	default:
		req.Scope.Actions = nil
	}

	tokenInfo, err := req.ToToken().ToResponse()
	if respondWithError(w, http.StatusBadRequest, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, tokenInfo)
}

func containsString(list []string, val string) bool {
	for _, v := range list {
		if v == val {
			return true
		}
	}
	return false
}

func filterRepoActions(actions []string, authz keppel.Authorization, account keppel.Account) (result []string) {
	for _, action := range actions {
		if action == "pull" && authz.HasPermission(keppel.CanPullFromAccount, account.AuthTenantID) {
			result = append(result, action)
		} else if action == "push" && authz.HasPermission(keppel.CanPushToAccount, account.AuthTenantID) {
			result = append(result, action)
		}
	}
	return
}

func (a *API) compileCatalogAccess(authz keppel.Authorization) ([]auth.Scope, error) {
	var accounts []keppel.Account
	_, err := a.db.Select(&accounts, "SELECT * FROM accounts ORDER BY name")
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
