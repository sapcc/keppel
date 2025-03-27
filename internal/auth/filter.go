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
	"database/sql"
	"errors"
	"fmt"

	. "github.com/majewsky/gg/option"
	"github.com/sapcc/go-bits/httpext"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
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
	var accounts []models.Account
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
			accounts = []models.Account{*account}
		}
	}

	for _, account := range accounts {
		if uid.HasPermission(keppel.CanViewAccount, account.AuthTenantID) {
			ss.Add(Scope{
				ResourceType: "keppel_account",
				ResourceName: string(account.Name),
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

	// NOTE: As an optimization, this only loads the few required fields for the account
	// instead of the entire `accounts` row. Before this optimization, the loads
	// via keppel.FindAccount() at this callsite made up 8% of all allocations
	// performed by keppel-api.
	var (
		authTenantID     string
		rbacPoliciesJSON string
	)
	err := db.QueryRow(
		`SELECT auth_tenant_id, rbac_policies_json FROM accounts WHERE name = $1`,
		repoScope.AccountName,
	).Scan(&authTenantID, &rbacPoliciesJSON)
	if errors.Is(err, sql.ErrNoRows) {
		// if the account does not exist, we cannot give access to it
		// (this is not an error, because an error would leak information on which accounts exist)
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	// collect permission overrides from matching RBAC policies
	policies, err := keppel.ParseRBACPoliciesField(rbacPoliciesJSON)
	if err != nil {
		return nil, fmt.Errorf("while parsing account RBAC policies: %w", err)
	}
	permOverride := make(map[keppel.RBACPermission]Option[bool])
	userName := uid.UserName()
	for _, policy := range policies {
		if !policy.Matches(ip, repoScope.RepositoryName, userName) {
			continue
		}
		// NOTE: forbidding overrides take precedence over granting overrides
		for _, perm := range policy.Permissions {
			if permOverride[perm] != Some(false) {
				permOverride[perm] = Some(true)
			}
		}
		for _, perm := range policy.ForbiddenPermissions {
			permOverride[perm] = Some(false)
		}
	}

	// certain policies can never be granted to anonymous users by an RBAC policy
	if uid.UserType() == keppel.AnonymousUser {
		delete(permOverride, keppel.RBACPullPermission)
		delete(permOverride, keppel.RBACPushPermission)
		delete(permOverride, keppel.RBACDeletePermission)
	}

	// evaluate final permission set
	isAllowedAction := map[string]bool{
		"pull": permOverride[keppel.RBACPullPermission].UnwrapOr(
			uid.HasPermission(keppel.CanPullFromAccount, authTenantID),
		),
		"push": permOverride[keppel.RBACPushPermission].UnwrapOr(
			uid.HasPermission(keppel.CanPushToAccount, authTenantID),
		),
		"delete": permOverride[keppel.RBACDeletePermission].UnwrapOr(
			uid.HasPermission(keppel.CanDeleteFromAccount, authTenantID),
		),
	}
	if permOverride[keppel.RBACAnonymousPullPermission].UnwrapOr(false) {
		isAllowedAction["pull"] = true
	}
	if isAllowedAction["pull"] {
		isAllowedAction["anonymous_first_pull"] = permOverride[keppel.RBACAnonymousFirstPullPermission].UnwrapOr(false)
	}

	// grant requested actions as possible
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
	if audience.AccountName != "" && scope.ResourceName != string(audience.AccountName) {
		// domain-remapped APIs only allow access to that API's account
		return nil, nil
	}

	if audience.IsAnycast {
		// defense in depth: any APIs requiring account-level permission are not anycastable anyway
		return nil, nil
	}

	account, err := keppel.FindAccount(db, models.AccountName(scope.ResourceName))
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
