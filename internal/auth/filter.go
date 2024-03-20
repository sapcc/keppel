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
	"fmt"

	"github.com/sapcc/go-bits/httpext"

	"github.com/sapcc/keppel/internal/keppel"
)

// Produces a new ScopeSet containing only those scopes that the given
// `uid` is permitted to access and only those actions therein which this `uid`
// is permitted to perform.
func filterAuthorized(ir IncomingRequest, uid keppel.UserIdentity, audience Audience, db *keppel.DB) (ScopeSet, error) {
	result := make(ScopeSet, 0, len(ir.Scopes))
	// make sure that additional scopes get appended at the end, on the offchance
	// that a client might parse its token and look at access[0] to check for its
	// authorization
	var additional ScopeSet

	var err error
	for _, scope := range ir.Scopes {
		filtered := *scope
		switch scope.ResourceType {
		case "registry":
			filtered.Actions, err = filterRegistryActions(uid, audience, db, scope, &additional)
			if err != nil {
				return nil, err
			}

		case "repository":
			ip := httpext.GetRequesterIPFor(ir.HTTPRequest)
			filtered.Actions, err = filterRepoActions(ip, *scope, uid, audience, db)
			if err != nil {
				return nil, err
			}

		case "keppel_api":
			switch {
			case scope.Contains(PeerAPIScope) && uid.UserType() == keppel.PeerUser:
				filtered.Actions = PeerAPIScope.Actions
			case scope.Contains(InfoAPIScope) && uid.UserType() != keppel.AnonymousUser:
				filtered.Actions = InfoAPIScope.Actions
			default:
				filtered.Actions = nil
			}

		case "keppel_account":
			filtered.Actions, err = filterKeppelAccountActions(uid, audience, db, scope)
			if err != nil {
				return nil, err
			}

		case "keppel_auth_tenant":
			switch {
			case audience.AccountName != "":
				// this type of scope is only used by the Keppel API, which does not
				// allow domain-remapping anyway
				filtered.Actions = nil
			case audience.IsAnycast:
				// defense in depth: any APIs requiring auth-tenant-level permission are not anycastable anyway
				filtered.Actions = nil
			default:
				filtered.Actions = filterAuthTenantActions(scope.ResourceName, scope.Actions, uid)
			}

		default:
			filtered.Actions = nil
		}
		result.Add(filtered)
	}

	return append(result, additional...), nil
}

func addCatalogAccess(ss *ScopeSet, uid keppel.UserIdentity, audience Audience, db *keppel.DB) error {
	var accounts []keppel.Account
	if audience.AccountName == "" {
		// on the standard API, all accounts are potentially accessible
		_, err := db.Select(&accounts, "SELECT * FROM accounts ORDER BY name")
		if err != nil {
			return err
		}
	} else {
		// on a domain-remapped API, only that API's account is accessible (if it exists)
		account, err := keppel.FindAccount(db, audience.AccountName)
		if err != nil {
			return err
		}
		if account != nil {
			accounts = []keppel.Account{*account}
		}
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

func filterRegistryActions(uid keppel.UserIdentity, audience Audience, db *keppel.DB, scope *Scope, additional *ScopeSet) ([]string, error) {
	var filtered []string

	if audience.IsAnycast {
		// we cannot allow catalog access on the anycast API since there is no way
		// to decide which peer does the authentication in this case
		return nil, nil
	}

	if uid.UserType() == keppel.AnonymousUser {
		// we don't allow catalog access to anonymous users:
		//
		// 1. if we did, nobody would ever be presented with the auth challenge
		// and thus all clients would assume that they get the same result
		// without auth (which is very much not true)
		//
		// 2. anon users do not get any keppel_account:*:view permissions, so it
		// does not help them to get access to the catalog endpoint anyway
		return nil, nil
	}

	if scope.Contains(CatalogEndpointScope) {
		filtered = CatalogEndpointScope.Actions
		err := addCatalogAccess(additional, uid, audience, db)
		if err != nil {
			return nil, err
		}
	}

	return filtered, nil
}

func filterRepoActions(ip string, scope Scope, uid keppel.UserIdentity, audience Audience, db *keppel.DB) ([]string, error) {
	repoScope := scope.ParseRepositoryScope(audience)
	if repoScope.RepositoryName == "" {
		// this happens when we are not on a domain-remapped API and thus expect a
		// scope.ResourceName of the form "account/repo", but we only got "account"
		// without any slashes
		return nil, nil
	}

	account, err := keppel.FindAccount(db, repoScope.AccountName)
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

	policies, err := account.ParseRBACPolicies()
	if err != nil {
		return nil, fmt.Errorf("while parsing account RBAC policies: %w", err)
	}
	userName := uid.UserName()
	for _, policy := range policies {
		if !policy.Matches(ip, repoScope.RepositoryName, userName) {
			continue
		}

		hasPerm := make(map[keppel.RBACPermission]bool)
		for _, perm := range policy.Permissions {
			hasPerm[perm] = true
		}

		if hasPerm[keppel.GrantsAnonymousPull] {
			isAllowedAction["pull"] = true
		}
		if hasPerm[keppel.GrantsAnonymousFirstPull] {
			isAllowedAction["anonymous_first_pull"] = true
		}
		if uid.UserType() != keppel.AnonymousUser {
			if hasPerm[keppel.GrantsPull] {
				isAllowedAction["pull"] = true
			}
			if hasPerm[keppel.GrantsPush] {
				isAllowedAction["push"] = true
			}
			if hasPerm[keppel.GrantsDelete] {
				isAllowedAction["delete"] = true
			}
		}
	}

	var result []string
	for _, action := range scope.Actions {
		if isAllowedAction[action] {
			result = append(result, action)
		}
		if action == "pull" && isAllowedAction["anonymous_first_pull"] {
			result = append(result, "anonymous_first_pull")
		}
	}
	return result, nil
}

func filterKeppelAccountActions(uid keppel.UserIdentity, audience Audience, db *keppel.DB, scope *Scope) ([]string, error) {
	if audience.AccountName != "" && scope.ResourceName != audience.AccountName {
		// domain-remapped APIs only allow access to that API's account
		return nil, nil
	}

	if audience.IsAnycast {
		// defense in depth: any APIs requiring account-level permission are not anycastable anyway
		return nil, nil
	}

	account, err := keppel.FindAccount(db, scope.ResourceName)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, nil
	}

	return filterAuthTenantActions(account.AuthTenantID, scope.Actions, uid), nil
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
