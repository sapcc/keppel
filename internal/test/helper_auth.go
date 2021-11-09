/******************************************************************************
*
*  Copyright 2021 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/tokenauth"
)

//GetToken obtains a token for use with the Registry V2 API.
//
//`scopes` is a list of token scopes, e.g. "repository:test1/foo:pull".
//The necessary permissions will be inferred from the given scopes, and a
//dummy UserIdentity object for the user called "correctusername" will be
//embedded in the token.
func (s Setup) GetToken(t *testing.T, scopes ...string) string {
	t.Helper()
	return s.getToken(t, auth.LocalService, scopes...)
}

//GetAnycastToken is like GetToken, but instead returns a token for the anycast
//endpoint.
func (s Setup) GetAnycastToken(t *testing.T, scopes ...string) string {
	t.Helper()
	return s.getToken(t, auth.AnycastService, scopes...)
}

func (s Setup) getToken(t *testing.T, audience auth.Service, scopes ...string) string {
	t.Helper()

	//optimization: don't issue the same token twice in a single test run
	cacheKey := strings.Join(append([]string{strconv.Itoa(int(audience))}, scopes...), "|")
	if token, exists := s.tokenCache[cacheKey]; exists {
		return token
	}

	//parse scopes
	var ss auth.ScopeSet
	for _, scopeStr := range scopes {
		fields := strings.SplitN(scopeStr, ":", 3)
		if len(fields) != 3 {
			t.Fatalf("malformed scope %q: needs exactly three colon-separated fields", scopeStr)
		}
		ss.Add(auth.Scope{
			ResourceType: fields[0],
			ResourceName: fields[1],
			Actions:      strings.Split(fields[2], ","),
		})
	}

	//translate scopes into required permissions
	perms := map[string]map[string]bool{
		string(keppel.CanViewAccount):       make(map[string]bool),
		string(keppel.CanPullFromAccount):   make(map[string]bool),
		string(keppel.CanPushToAccount):     make(map[string]bool),
		string(keppel.CanDeleteFromAccount): make(map[string]bool),
	}
	for _, scope := range ss {
		switch scope.ResourceType {
		case "registry":
			if scope.String() != "registry:catalog:*" {
				t.Fatalf("do not know how to handle scope %q", scope.String())
			}
		case "repository":
			authTenantID, err := s.findAuthTenantIDForAccountName(strings.SplitN(scope.ResourceName, "/", 2)[0])
			must(t, err)
			perms[string(keppel.CanViewAccount)][authTenantID] = true
			for _, action := range scope.Actions {
				switch action {
				case "pull":
					perms[string(keppel.CanPullFromAccount)][authTenantID] = true
				case "push":
					perms[string(keppel.CanPushToAccount)][authTenantID] = true
				case "delete":
					perms[string(keppel.CanDeleteFromAccount)][authTenantID] = true
				default:
					t.Fatalf("do not know how to handle action %q in scope %q", action, scope.String())
				}
			}
		case "keppel_account":
			if strings.Join(scope.Actions, ",") != "view" {
				t.Fatalf("do not know how to handle scope %q", scope.String())
			}
			authTenantID, err := s.findAuthTenantIDForAccountName(scope.ResourceName)
			must(t, err)
			perms[string(keppel.CanViewAccount)][authTenantID] = true
		}
	}

	//convert []*auth.Scope into []auth.Scope
	var flatScopes []auth.Scope
	for _, scope := range ss {
		flatScopes = append(flatScopes, *scope)
	}

	//issue token
	issuedToken, err := tokenauth.Token{
		UserIdentity: userIdentity{
			Username: "correctusername",
			Perms:    perms,
		},
		Audience: audience,
		Access:   flatScopes,
	}.Issue(s.Config)
	must(t, err)

	s.tokenCache[cacheKey] = issuedToken.SignedToken
	return issuedToken.SignedToken
}

func (s Setup) findAuthTenantIDForAccountName(accountName string) (string, error) {
	//optimization: if we can find this specific account in the list of
	//pre-provisioned accounts, we can skip the DB lookup
	for _, a := range s.Accounts {
		if a.Name == accountName {
			return a.AuthTenantID, nil
		}
	}

	//base case: look up in the DB
	return s.DB.SelectStr(`SELECT auth_tenant_id FROM accounts WHERE name = $1`, accountName)
}
