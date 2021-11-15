/*******************************************************************************
*
* Copyright 2020 SAP SE
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

package auth

import (
	"strings"

	"github.com/sapcc/keppel/internal/keppel"
)

//Produces a new ScopeSet containing only those scopes that the given
//`uid` is permitted to access and only those actions therein which this `uid`
//is permitted to perform.
func filterAuthorized(ss ScopeSet, uid keppel.UserIdentity, audience Service, db *keppel.DB) (ScopeSet, error) {
	result := make(ScopeSet, 0, len(ss))
	//make sure that additional scopes get appended at the end, on the offchance
	//that a client might parse its token and look at access[0] to check for its
	//authorization
	var additional ScopeSet

	var err error
	for _, scope := range ss {
		filtered := *scope
		switch scope.ResourceType {
		case "registry":
			if audience == AnycastService {
				//we cannot allow catalog access on the anycast API since there is no way
				//to decide which peer does the authentication in this case
				filtered.Actions = nil
			} else if uid.UserType() == keppel.AnonymousUser {
				//we don't allow catalog access to anonymous users:
				//
				//1. if we did, nobody would ever be presented with the auth challenge
				//and thus all clients would assume that they get the same result
				//without auth (which is very much not true)
				//
				//2. anon users do not get any keppel_account:*:view permissions, so it
				//does not help them to get access to the catalog endpoint anyway
				filtered.Actions = nil
			} else if scope.Contains(CatalogEndpointScope) {
				filtered.Actions = CatalogEndpointScope.Actions
				err = addCatalogAccess(&additional, uid, db)
				if err != nil {
					return nil, err
				}
			} else {
				filtered.Actions = nil
			}

		case "repository":
			if !strings.Contains(scope.ResourceName, "/") {
				//just an account name does not make a repository name
				filtered.Actions = nil
			} else {
				filtered.Actions, err = filterRepoActions(*scope, uid, db)
				if err != nil {
					return nil, err
				}
			}

		case "keppel_api":
			if scope.Contains(PeerAPIScope) && uid.UserType() == keppel.PeerUser {
				filtered.Actions = PeerAPIScope.Actions
			} else if scope.Contains(InfoAPIScope) && uid.UserType() != keppel.AnonymousUser {
				filtered.Actions = InfoAPIScope.Actions
			} else {
				filtered.Actions = nil
			}

		case "keppel_account":
			account, err := keppel.FindAccount(db, scope.ResourceName)
			if err != nil {
				return nil, err
			}
			if account == nil {
				filtered.Actions = nil
			} else {
				filtered.Actions = filterAuthTenantActions(account.AuthTenantID, scope.Actions, uid)
			}

		case "keppel_auth_tenant":
			filtered.Actions = filterAuthTenantActions(scope.ResourceName, scope.Actions, uid)

		default:
			filtered.Actions = nil
		}
		result.Add(filtered)
	}

	return append(result, additional...), nil
}

func addCatalogAccess(ss *ScopeSet, uid keppel.UserIdentity, db *keppel.DB) error {
	var accounts []keppel.Account
	_, err := db.Select(&accounts, "SELECT * FROM accounts ORDER BY name")
	if err != nil {
		return err
	}

	for _, account := range accounts {
		if uid.HasPermission(keppel.CanViewAccount, account.AuthTenantID) {
			ss.Add(Scope{
				ResourceType: "keppel_account",
				ResourceName: account.Name,
				Actions:      []string{"view"},
			})
		}
	}

	return nil
}

func filterRepoActions(scope Scope, uid keppel.UserIdentity, db *keppel.DB) ([]string, error) {
	account, err := keppel.FindAccount(db, scope.AccountName())
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, nil
	}

	isAllowedAction := map[string]bool{
		"pull":   uid.HasPermission(keppel.CanPullFromAccount, account.AuthTenantID),
		"push":   uid.HasPermission(keppel.CanPushToAccount, account.AuthTenantID),
		"delete": uid.HasPermission(keppel.CanDeleteFromAccount, account.AuthTenantID),
	}

	var policies []keppel.RBACPolicy
	_, err = db.Select(&policies, "SELECT * FROM rbac_policies WHERE account_name = $1", account.Name)
	if err != nil {
		return nil, err
	}
	userName := uid.UserName()
	for _, policy := range policies {
		if policy.Matches(scope.ResourceName, userName) {
			if policy.CanPullAnonymously {
				isAllowedAction["pull"] = true
			}
			if policy.CanPull && uid.UserType() != keppel.AnonymousUser {
				isAllowedAction["pull"] = true
			}
			if policy.CanPush && uid.UserType() != keppel.AnonymousUser {
				isAllowedAction["push"] = true
			}
			if policy.CanDelete && uid.UserType() != keppel.AnonymousUser {
				isAllowedAction["delete"] = true
			}
		}
	}

	var result []string
	for _, action := range scope.Actions {
		if isAllowedAction[action] {
			result = append(result, action)
		}
	}
	return result, nil
}

func filterAuthTenantActions(authTenantID string, actions []string, uid keppel.UserIdentity) []string {
	if authTenantID == "" {
		return nil
	}

	isAllowedAction := map[string]bool{
		"view":        uid.HasPermission(keppel.CanViewAccount, authTenantID),
		"change":      uid.HasPermission(keppel.CanChangeAccount, authTenantID),
		"viewquota":   uid.HasPermission(keppel.CanViewQuotas, authTenantID),
		"changequota": uid.HasPermission(keppel.CanChangeQuotas, authTenantID),
	}

	var result []string
	for _, action := range actions {
		if isAllowedAction[action] {
			result = append(result, action)
		}
	}
	return result
}