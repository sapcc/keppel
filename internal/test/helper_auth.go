// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// FlattenHeaders converts a http.Header into map[string]string.
// This is a temporary helper function to support old assert.HTTPRequest usage.
//
// TODO: when refactoring callsites into httptest.Handler.RespondTo(), convert:
// - calls without variadic arguments into just `httptest.WithHeaders(hdr)`
// - calls with variadic arguments into `httptest.WithHeaders(hdr)` plus one `httptest.WithHeader(key, value)` per extra header
//
// TODO: remove once refactoring from assert.HTTPRequest to httptest.Handler.RespondTo() is complete
func FlattenHeaders(hdr http.Header, extraHeaders ...map[string]string) map[string]string {
	result := make(map[string]string, len(hdr))
	for k, v := range hdr {
		if len(v) > 0 {
			result[k] = v[0]
		}
	}
	for _, extraHeader := range extraHeaders {
		maps.Copy(result, extraHeader)
	}
	return result
}

// GetTokenHeaders obtains a token for use with the Registry V2 API.
//
// `scopes` is a list of token scopes, e.g. "repository:test1/foo:pull".
// The necessary permissions will be inferred from the given scopes, and a
// dummy UserIdentity object for the user called "correctusername" will be
// embedded in the token.
//
// The return value contains the `Authorization` header for this token.
func (s Setup) GetTokenHeaders(t *testing.T, scopes ...string) http.Header {
	t.Helper()
	token := s.getToken(t, auth.Audience{IsAnycast: false}, scopes...)
	return http.Header{
		"Authorization": {"Bearer " + token},
	}
}

// GetAnycastTokenHeaders is like GetTokenHeaders, but instead returns a token for the anycast endpoint.
//
// The return value contains the `Authorization` header for this token,
// as well as `X-Forwarded-*` headers that select the anycast API.
func (s Setup) GetAnycastTokenHeaders(t *testing.T, scopes ...string) http.Header {
	t.Helper()
	token := s.getToken(t, auth.Audience{IsAnycast: true}, scopes...)
	return http.Header{
		"Authorization":     {"Bearer " + token},
		"X-Forwarded-Host":  {s.Config.AnycastAPIPublicHostname},
		"X-Forwarded-Proto": {"https"},
	}
}

// GetDomainRemappedTokenHeaders is like GetTokenHeaders, but instead returns a token for a domain-remapped API.
//
// The return value contains the `Authorization` header for this token,
// as well as `X-Forwarded-*` headers that select the domain-remapped API.
func (s Setup) GetDomainRemappedTokenHeaders(t *testing.T, accountName models.AccountName, scopes ...string) http.Header {
	t.Helper()
	token := s.getToken(t, auth.Audience{IsAnycast: false, AccountName: accountName}, scopes...)
	return http.Header{
		"Authorization":     {"Bearer " + token},
		"X-Forwarded-Host":  {fmt.Sprintf("%s.%s", accountName, s.Config.APIPublicHostname)},
		"X-Forwarded-Proto": {"https"},
	}
}

func (s Setup) getToken(t *testing.T, audience auth.Audience, scopes ...string) string {
	t.Helper()

	//optimization: don't issue the same token twice in a single test run
	audienceJSON := must.ReturnT(json.Marshal(audience))(t)
	cacheKey := string(audienceJSON) + strings.Join(scopes, "|")
	if token, exists := s.tokenCache[cacheKey]; exists {
		return token
	}

	// parse scopes
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

	// translate scopes into required permissions
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
			authTenantID := must.ReturnT(s.findAuthTenantIDForAccountName(repoScope.AccountName))(t)
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
			authTenantID := must.ReturnT(s.findAuthTenantIDForAccountName(models.AccountName(scope.ResourceName)))(t)
			perms[string(keppel.CanViewAccount)][authTenantID] = true
		}
	}

	// issue token
	tokenResp := must.ReturnT(auth.Authorization{
		UserIdentity: &userIdentity{
			Username: "correctusername",
			Perms:    perms,
		},
		Audience: audience,
		ScopeSet: ss,
	}.IssueToken(s.Config))(t)

	s.tokenCache[cacheKey] = tokenResp.Token
	return tokenResp.Token
}

// GetAnonTokenHeaders obtains an anonymous token for use with the Registry V2 API.
//
// `scopes` is a list of token scopes, e.g. "repository:test1/foo:pull".
// The necessary permissions will be inferred from the given scopes.
//
// The return value contains the `Authorization` header for this token.
func (s Setup) GetAnonTokenHeaders(t *testing.T, repo string, scopes []string) http.Header {
	t.Helper()

	resp, tokenBodyBytes := assert.HTTPRequest{
		Method: "GET",
		Path:   fmt.Sprintf("/keppel/v1/auth?service=%s&scope=%s:%s", s.Config.APIPublicHostname, repo, strings.Join(scopes, ",")),
		Header: map[string]string{
			"X-Forwarded-Host":  s.Config.APIPublicHostname,
			"X-Forwarded-Proto": "https",
		},
		ExpectStatus: http.StatusOK,
	}.Check(t, s.Handler)
	resp.Body.Close()
	var tokenBodyData struct {
		Token string `json:"token"`
	}
	must.SucceedT(t, json.Unmarshal(tokenBodyBytes, &tokenBodyData))

	return http.Header{
		"Authorization": {"Bearer " + tokenBodyData.Token},
	}
}

func (s Setup) findAuthTenantIDForAccountName(accountName models.AccountName) (string, error) {
	//optimization: if we can find this specific account in the list of
	// pre-provisioned accounts, we can skip the DB lookup
	for _, a := range s.Accounts {
		if a.Name == accountName {
			return a.AuthTenantID, nil
		}
	}

	// base case: look up in the DB
	return s.DB.SelectStr(`SELECT auth_tenant_id FROM accounts WHERE name = $1`, accountName)
}
