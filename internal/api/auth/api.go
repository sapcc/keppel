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
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/go-bits/sre"
	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
)

//API contains state variables used by the Auth API endpoint.
type API struct {
	cfg        keppel.Configuration
	authDriver keppel.AuthDriver
	fd         keppel.FederationDriver
	db         *keppel.DB
}

//NewAPI constructs a new API instance.
func NewAPI(cfg keppel.Configuration, ad keppel.AuthDriver, fd keppel.FederationDriver, db *keppel.DB) *API {
	return &API{cfg, ad, fd, db}
}

//AddTo implements the api.API interface.
func (a *API) AddTo(r *mux.Router) {
	r.Methods("GET").Path("/keppel/v1/auth").HandlerFunc(a.handleGetAuth)
	r.Methods("POST").Path("/keppel/v1/auth/peering").HandlerFunc(a.handlePostPeering)
}

func respondWithError(w http.ResponseWriter, code int, err error) bool {
	if rerr, ok := err.(*keppel.RegistryV2Error); ok {
		if rerr != nil {
			rerr.WriteAsAuthResponseTo(w)
			return true
		}
		return false
	}

	if err != nil {
		respondwith.JSON(w, code, map[string]string{"details": err.Error()})
		return true
	}
	return false
}

var errUnautorized = errors.New("incorrect username or password")

func (a *API) handleGetAuth(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/keppel/v1/auth")

	//parse request
	req, err := parseRequest(r.URL.RawQuery, a.cfg)
	if respondWithError(w, http.StatusBadRequest, err) {
		return
	}

	//special cases for anycast requests
	if req.IntendedAudience == auth.AnycastService {
		if len(req.Scopes) > 1 {
			//NOTE: This is not a fundamental restriction, there was just no demand for
			//it yet. If the requirement comes up, we could ask all relevant upstreams
			//for tokens and issue one token that grants the sum of all accesses.
			respondWithError(w, http.StatusInternalServerError, errors.New("anycast tokens cannot be issued for multiple scopes at once"))
			return
		}

		if len(req.Scopes) == 1 {
			scope := req.Scopes[0]
			if scope.ResourceType == "repository" {
				account, err := keppel.FindAccount(a.db, scope.AccountName())
				if respondWithError(w, http.StatusInternalServerError, err) {
					return
				}

				//if we don't have this account locally, but the request is an anycast
				//request and one of our peers has the account, ask them to issue the token
				if account == nil {
					err := a.reverseProxyTokenReqToUpstream(w, r, req, scope.AccountName())
					if err != keppel.ErrNoSuchPrimaryAccount {
						respondWithError(w, http.StatusInternalServerError, err)
						return
					}
				}
			}
		}
	}

	//check authentication
	authz, err := a.checkAuthentication(r.Header.Get("Authorization"))
	if respondWithError(w, http.StatusUnauthorized, err) {
		return
	}

	//check requested scope and actions
	for _, scope := range req.Scopes {
		switch scope.ResourceType {
		case "registry":
			if req.IntendedAudience == auth.AnycastService {
				//we cannot allow catalog access on the anycast API since there is no way
				//to decide which peer does the authentication in this case
				scope.Actions = nil
			} else if scope.ResourceName == "catalog" && containsString(scope.Actions, "*") {
				scope.Actions = []string{"*"}
				err = a.compileCatalogAccess(authz, req.CompiledScopes.Add)
				if respondWithError(w, http.StatusInternalServerError, err) {
					return
				}
			} else {
				scope.Actions = nil
			}
		case "repository":
			account, err := keppel.FindAccount(a.db, scope.AccountName())
			if respondWithError(w, http.StatusInternalServerError, err) {
				return
			}
			if account == nil || !strings.Contains(scope.ResourceName, "/") {
				scope.Actions = nil
			} else {
				scope.Actions, err = a.filterRepoActions(scope.ResourceName, scope.Actions, authz, *account)
				if respondWithError(w, http.StatusInternalServerError, err) {
					return
				}
			}
		default:
			scope.Actions = nil
		}
	}

	tokenInfo, err := makeTokenResponse(req.ToToken(authz), a.cfg)
	if respondWithError(w, http.StatusBadRequest, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, tokenInfo)
}

func (a *API) reverseProxyTokenReqToUpstream(w http.ResponseWriter, r *http.Request, tokenReq Request, accountName string) error {
	primaryHostName, err := a.fd.FindPrimaryAccount(accountName)
	if err != nil {
		return err
	}

	//protect against infinite forwarding loops in case different Keppels have
	//different ideas about how is the primary account
	if forwardedBy := r.URL.Query().Get("X-Keppel-Forwarded-By"); forwardedBy != "" {
		logg.Error("not forwarding anycast token request for account %q to %s because request was already forwarded to us by %s",
			accountName, primaryHostName, forwardedBy)
		return errors.New("request blocked by reverse-proxy loop protection")
	}

	return a.cfg.ReverseProxyAnycastRequestToPeer(w, r, primaryHostName)
}

func containsString(list []string, val string) bool {
	for _, v := range list {
		if v == val {
			return true
		}
	}
	return false
}

func (a *API) filterRepoActions(repoName string, actions []string, authz keppel.Authorization, account keppel.Account) ([]string, error) {
	isAllowedAction := map[string]bool{
		"pull":   authz.HasPermission(keppel.CanPullFromAccount, account.AuthTenantID),
		"push":   authz.HasPermission(keppel.CanPushToAccount, account.AuthTenantID),
		"delete": authz.HasPermission(keppel.CanDeleteFromAccount, account.AuthTenantID),
	}

	var policies []keppel.RBACPolicy
	_, err := a.db.Select(&policies, "SELECT * FROM rbac_policies WHERE account_name = $1", account.Name)
	if err != nil {
		return nil, err
	}
	userName := authz.UserName()
	for _, policy := range policies {
		if policy.Matches(repoName, userName) {
			if policy.CanPullAnonymously {
				isAllowedAction["pull"] = true
			}
			if policy.CanPull && authz != keppel.AnonymousAuthorization {
				isAllowedAction["pull"] = true
			}
			if policy.CanPush && authz != keppel.AnonymousAuthorization {
				isAllowedAction["push"] = true
			}
			if policy.CanDelete && authz != keppel.AnonymousAuthorization {
				isAllowedAction["delete"] = true
			}
		}
	}

	var result []string
	for _, action := range actions {
		if isAllowedAction[action] {
			result = append(result, action)
		}
	}
	return result, nil
}

func (a *API) compileCatalogAccess(authz keppel.Authorization, addScope func(auth.Scope)) error {
	var accounts []keppel.Account
	_, err := a.db.Select(&accounts, "SELECT * FROM accounts ORDER BY name")
	if err != nil {
		return err
	}

	for _, account := range accounts {
		if authz.HasPermission(keppel.CanViewAccount, account.AuthTenantID) {
			addScope(auth.Scope{
				ResourceType: "keppel_account",
				ResourceName: account.Name,
				Actions:      []string{"view"},
			})
		}
	}

	return nil
}
