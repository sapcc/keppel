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

package tokenauth

import (
	"strings"

	"github.com/sapcc/keppel/internal/keppel"
)

//ScopeSet is a set of scopes.
type ScopeSet []*Scope

//Contains returns true if the given token authorizes the user for this scope.
func (ss ScopeSet) Contains(s Scope) bool {
	for _, scope := range ss {
		if scope.Contains(s) {
			return true
		}
	}
	return false
}

//Add adds a scope to this ScopeSet. If the ScopeSet already contains a Scope
//referring to the same resource, it is merged with the given scope.
func (ss *ScopeSet) Add(s Scope) {
	for _, other := range *ss {
		if s.ResourceType == other.ResourceType && s.ResourceName == other.ResourceName {
			other.Actions = mergeAndDedupActions(other.Actions, s.Actions)
			return
		}
	}
	*ss = append(*ss, &s)
}

func mergeAndDedupActions(lhs, rhs []string) (result []string) {
	seen := make(map[string]bool)
	for _, elem := range append(lhs, rhs...) {
		if seen[elem] {
			continue
		}
		result = append(result, elem)
		seen[elem] = true
	}
	return
}

//FilterAuthorized produces a new ScopeSet containing only those scopes
//that the given `authz` is permitted to access and only those actions therein
//which this `authz` is permitted to perform.
func (ss ScopeSet) FilterAuthorized(authz keppel.Authorization, audience Service, db *keppel.DB) (ScopeSet, error) {
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
			} else if scope.ResourceName == "catalog" && containsString(scope.Actions, "*") {
				filtered.Actions = []string{"*"}
				err = additional.addCatalogAccess(authz, db)
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
				filtered.Actions, err = filterRepoActions(*scope, authz, db)
				if err != nil {
					return nil, err
				}
			}
		default:
			filtered.Actions = nil
		}
		result.Add(filtered)
	}

	return append(result, additional...), nil
}

func containsString(list []string, val string) bool {
	for _, v := range list {
		if v == val {
			return true
		}
	}
	return false
}

func (ss *ScopeSet) addCatalogAccess(authz keppel.Authorization, db *keppel.DB) error {
	var accounts []keppel.Account
	_, err := db.Select(&accounts, "SELECT * FROM accounts ORDER BY name")
	if err != nil {
		return err
	}

	for _, account := range accounts {
		if authz.HasPermission(keppel.CanViewAccount, account.AuthTenantID) {
			ss.Add(Scope{
				ResourceType: "keppel_account",
				ResourceName: account.Name,
				Actions:      []string{"view"},
			})
		}
	}

	return nil
}

func filterRepoActions(scope Scope, authz keppel.Authorization, db *keppel.DB) ([]string, error) {
	account, err := keppel.FindAccount(db, scope.AccountName())
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, nil
	}

	isAllowedAction := map[string]bool{
		"pull":   authz.HasPermission(keppel.CanPullFromAccount, account.AuthTenantID),
		"push":   authz.HasPermission(keppel.CanPushToAccount, account.AuthTenantID),
		"delete": authz.HasPermission(keppel.CanDeleteFromAccount, account.AuthTenantID),
	}

	var policies []keppel.RBACPolicy
	_, err = db.Select(&policies, "SELECT * FROM rbac_policies WHERE account_name = $1", account.Name)
	if err != nil {
		return nil, err
	}
	userName := authz.UserName()
	for _, policy := range policies {
		if policy.Matches(scope.ResourceName, userName) {
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
	for _, action := range scope.Actions {
		if isAllowedAction[action] {
			result = append(result, action)
		}
	}
	return result, nil
}
