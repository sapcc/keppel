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
	"encoding/json"
	"strings"
	"testing"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
)

//GetToken obtains a token for use with the Registry V2 API.
//
//`scopes` is a list of token scopes, e.g. "repository:test1/foo:pull".
//The necessary permissions will be inferred from the given scopes, and a
//dummy UserIdentity object for the user called "correctusername" will be
//embedded in the token.
func (s Setup) GetToken(t *testing.T, scopes ...string) string {
	t.Helper()
	return s.getToken(t, auth.Audience{IsAnycast: false}, scopes...)
}

//GetAnycastToken is like GetToken, but instead returns a token for the anycast
//endpoint.
func (s Setup) GetAnycastToken(t *testing.T, scopes ...string) string {
	t.Helper()
	return s.getToken(t, auth.Audience{IsAnycast: true}, scopes...)
}

//GetDomainRemappedToken is like GetToken, but instead returns a token for a
//domain-remapped API.
func (s Setup) GetDomainRemappedToken(t *testing.T, accountName string, scopes ...string) string {
	t.Helper()
	return s.getToken(t, auth.Audience{IsAnycast: false, AccountName: accountName}, scopes...)
}

func (s Setup) getToken(t *testing.T, audience auth.Audience, scopes ...string) string {
	t.Helper()

	//optimization: don't issue the same token twice in a single test run
	audienceJSON, err := json.Marshal(audience)
	mustDo(t, err)
	cacheKey := string(audienceJSON) + strings.Join(scopes, "|")
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
			repoScope := scope.ParseRepositoryScope(audience)
			authTenantID, err := s.findAuthTenantIDForAccountName(repoScope.AccountName)
			mustDo(t, err)
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
			mustDo(t, err)
			perms[string(keppel.CanViewAccount)][authTenantID] = true
		}
	}

	//issue token
	tokenResp, err := auth.Authorization{
		UserIdentity: userIdentity{
			Username: "correctusername",
			Perms:    perms,
		},
		Audience: audience,
		ScopeSet: ss,
	}.IssueToken(s.Config)
	mustDo(t, err)

	s.tokenCache[cacheKey] = tokenResp.Token
	return tokenResp.Token
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

//AddHeadersForCorrectAuthChallenge adds X-Forwarded-... headers to a request
//to ensure that the correct auth challenge gets generated. It won't work
//without these headers since the httptest library sets "Host: example.com" on
//all simulated HTTP requests.
func AddHeadersForCorrectAuthChallenge(hdr map[string]string) map[string]string {
	if hdr == nil {
		hdr = make(map[string]string)
	}
	hdr["X-Forwarded-Host"] = "registry.example.org"
	hdr["X-Forwarded-Proto"] = "https"
	return hdr
}
