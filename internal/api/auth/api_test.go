// SPDX-FileCopyrightText: 2019-2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package authapi_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/httptest"
	"github.com/sapcc/go-bits/must"
	"go.xyrillian.de/gg/jsonmatch"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestMain(m *testing.M) {
	easypg.WithTestDB(m, func() int { return m.Run() })
}

////////////////////////////////////////////////////////////////////////////////
// The testcases in this file encode a lot of knowledge that I gained by
// torturing the auth API of Docker Hub. DO NOT CHANGE stuff unless you have
// verified how the Docker Hub auth endpoint works.
// For the record, the auth endpoint of Docker Hub can be found by
//
//     curl -si https://index.docker.io/v2/ | grep Authenticate

type TestCase struct {
	// request
	Scope          string
	AnonymousLogin bool
	// situation
	CannotPush   bool
	CannotPull   bool
	CannotDelete bool
	RBACPolicy   *keppel.RBACPolicy
	// result
	GrantedActions   string
	AdditionalScopes []string
}

var (
	policyAnonPull = keppel.RBACPolicy{
		RepositoryPattern: "fo+",
		Permissions:       []keppel.RBACPermission{keppel.RBACAnonymousPullPermission},
	}
	policyAnonFirstPull = keppel.RBACPolicy{
		RepositoryPattern: "fo+",
		Permissions:       []keppel.RBACPermission{keppel.RBACAnonymousPullPermission, keppel.RBACAnonymousFirstPullPermission},
	}
	policyPullMatches = keppel.RBACPolicy{
		RepositoryPattern: "fo+",
		UserNamePattern:   "correct.*",
		Permissions:       []keppel.RBACPermission{keppel.RBACPullPermission},
	}
	policyForbidPush = keppel.RBACPolicy{
		RepositoryPattern:    "fo+",
		UserNamePattern:      "correct.*",
		ForbiddenPermissions: []keppel.RBACPermission{keppel.RBACPushPermission},
	}
	policyPushMatches = keppel.RBACPolicy{
		RepositoryPattern: "fo+",
		UserNamePattern:   "correct.*",
		Permissions:       []keppel.RBACPermission{keppel.RBACPullPermission, keppel.RBACPushPermission},
	}
	policyDeleteMatches = keppel.RBACPolicy{
		RepositoryPattern: "fo+",
		UserNamePattern:   "correct.*",
		Permissions:       []keppel.RBACPermission{keppel.RBACPullPermission, keppel.RBACDeletePermission},
	}
	policyPullDoesNotMatch = keppel.RBACPolicy{
		RepositoryPattern: "fo+",
		UserNamePattern:   "doesnotmatch",
		Permissions:       []keppel.RBACPermission{keppel.RBACPullPermission},
	}
	policyPushDoesNotMatch = keppel.RBACPolicy{
		RepositoryPattern: "doesnotmatch",
		UserNamePattern:   "correct.*",
		Permissions:       []keppel.RBACPermission{keppel.RBACPullPermission, keppel.RBACPushPermission},
	}
	policyDeleteDoesNotMatch = keppel.RBACPolicy{
		RepositoryPattern: "fo+",
		UserNamePattern:   "doesnotmatch",
		Permissions:       []keppel.RBACPermission{keppel.RBACPullPermission, keppel.RBACDeletePermission},
	}
)

var testCases = []TestCase{
	// basic success case
	{Scope: "repository:test1/foo:pull",
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		GrantedActions: "push"},
	{Scope: "repository:test1/foo:pull,push",
		GrantedActions: "pull,push"},
	{Scope: "repository:test1/foo:delete",
		GrantedActions: "delete"},
	// not allowed to pull
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, GrantedActions: ""},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, GrantedActions: "push"},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, GrantedActions: "push"},
	{Scope: "repository:test1/foo:delete",
		CannotPull: true, GrantedActions: "delete"},
	// anonymous first pull is not granted if the user is not allowed to pull
	{Scope: "repository:test1/foo:anonymous_first_pull", AnonymousLogin: true,
		CannotPull: true},
	// not allowed to push
	{Scope: "repository:test1/foo:pull",
		CannotPush: true, GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		CannotPush: true, GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		CannotPush: true, GrantedActions: "pull"},
	{Scope: "repository:test1/foo:delete",
		CannotPush: true, GrantedActions: "delete"},
	// not allowed to pull nor push
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, CannotPush: true, GrantedActions: ""},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, CannotPush: true, GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, CannotPush: true, GrantedActions: ""},
	{Scope: "repository:test1/foo:delete",
		CannotPull: true, CannotPush: true, GrantedActions: "delete"},
	// not allowed to delete
	{Scope: "repository:test1/foo:pull",
		CannotDelete: true, GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		CannotDelete: true, GrantedActions: "push"},
	{Scope: "repository:test1/foo:pull,push",
		CannotDelete: true, GrantedActions: "pull,push"},
	{Scope: "repository:test1/foo:delete",
		CannotDelete: true, GrantedActions: ""},
	// catalog access always allowed if username/password are ok (access to
	// specific accounts is filtered later)
	{Scope: "registry:catalog:*",
		GrantedActions:   "*",
		AdditionalScopes: []string{"keppel_account:test1:view"}},
	{Scope: "registry:catalog:*",
		CannotPull: true, GrantedActions: "*"},
	{Scope: "registry:catalog:*",
		CannotPush: true, GrantedActions: "*",
		AdditionalScopes: []string{"keppel_account:test1:view"}},
	{Scope: "registry:catalog:*",
		CannotPull: true, CannotPush: true, GrantedActions: "*"},
	{Scope: "registry:catalog:*",
		CannotDelete: true, GrantedActions: "*",
		AdditionalScopes: []string{"keppel_account:test1:view"}},
	// unknown resources/actions for resource type "registry"
	{Scope: "registry:test1/foo:pull",
		GrantedActions: ""},
	{Scope: "registry:catalog:pull",
		GrantedActions: ""},
	// incomplete scope syntax
	{Scope: "",
		GrantedActions: ""},
	{Scope: "repository",
		GrantedActions: ""},
	{Scope: "repository:",
		GrantedActions: ""},
	{Scope: "repository:test1",
		GrantedActions: ""},
	{Scope: "repository:test1/",
		GrantedActions: ""},
	{Scope: "repository:test1/foo",
		GrantedActions: ""},
	{Scope: "repository:test1/foo:",
		GrantedActions: ""},
	{Scope: "repository:test1:pull",
		GrantedActions: ""},
	{Scope: "repository:test1/:pull",
		GrantedActions: ""},
	// invalid scope syntax (overlong repository name)
	{Scope: fmt.Sprintf("repository:test1/%s:pull", strings.Repeat("a", 300)),
		GrantedActions: ""},
	// invalid scope syntax (malformed repository name)
	{Scope: "repository:test1/???:pull",
		GrantedActions: ""},
	// anonymous login when RBAC policies do not allow access
	{Scope: "repository:test1/foo:pull", AnonymousLogin: true,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:push", AnonymousLogin: true,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push", AnonymousLogin: true,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:delete", AnonymousLogin: true,
		GrantedActions: ""},
	// anonymous pull (but not push) is allowed by a matching RBAC policy
	{Scope: "repository:test1/foo:pull", AnonymousLogin: true,
		RBACPolicy:     &policyAnonPull,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push", AnonymousLogin: true,
		RBACPolicy:     &policyAnonPull,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push", AnonymousLogin: true,
		RBACPolicy:     &policyAnonPull,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:delete", AnonymousLogin: true,
		RBACPolicy:     &policyAnonPull,
		GrantedActions: ""},
	// RBAC policy with RepositoryPattern only works when repository name matches
	{Scope: "repository:test1/foobar:pull", AnonymousLogin: true,
		RBACPolicy:     &policyAnonPull,
		GrantedActions: ""},
	{Scope: "repository:test1/foobar:push", AnonymousLogin: true,
		RBACPolicy:     &policyAnonPull,
		GrantedActions: ""},
	{Scope: "repository:test1/foobar:pull,push", AnonymousLogin: true,
		RBACPolicy:     &policyAnonPull,
		GrantedActions: ""},
	{Scope: "repository:test1/foobar:delete", AnonymousLogin: true,
		RBACPolicy:     &policyAnonPull,
		GrantedActions: ""},
	// RBAC policy for anonymous pull also enables pull access for all authenticated users
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyAnonPull,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyAnonPull,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyAnonPull,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:delete",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyAnonPull,
		GrantedActions: ""},
	// RBAC policy for anonymous pull does not change anything if the user already has pull access
	{Scope: "repository:test1/foo:pull",
		RBACPolicy:     &policyAnonPull,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		RBACPolicy:     &policyAnonPull,
		GrantedActions: "push"},
	{Scope: "repository:test1/foo:pull,push",
		RBACPolicy:     &policyAnonPull,
		GrantedActions: "pull,push"},
	{Scope: "repository:test1/foo:delete",
		RBACPolicy:     &policyAnonPull,
		GrantedActions: "delete"},
	// anonymous first pull is allowed by a matching RBAC policy
	{Scope: "repository:test1/foo:pull", AnonymousLogin: true,
		RBACPolicy:     &policyAnonFirstPull,
		GrantedActions: "pull,anonymous_first_pull"},
	{Scope: "repository:test1/foo:push", AnonymousLogin: true,
		RBACPolicy:     &policyAnonFirstPull,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push", AnonymousLogin: true,
		RBACPolicy:     &policyAnonFirstPull,
		GrantedActions: "pull,anonymous_first_pull"},
	{Scope: "repository:test1/foo:delete", AnonymousLogin: true,
		RBACPolicy:     &policyAnonFirstPull,
		GrantedActions: ""},
	// anonymous_first_pull does not grant additional scopes
	{Scope: "repository:test1/foo:anonymous_first_pull", AnonymousLogin: true,
		RBACPolicy:     &policyAnonFirstPull,
		GrantedActions: "anonymous_first_pull"},
	// RBAC policy with CanPull grants pull permissions to matching users
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyPullMatches,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyPullMatches,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyPullMatches,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:delete",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyPullMatches,
		GrantedActions: ""},
	// RBAC policy with CanPull does not grant permissions if it does not match
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyPullDoesNotMatch,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyPullDoesNotMatch,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyPullDoesNotMatch,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:delete",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyPullDoesNotMatch,
		GrantedActions: ""},
	// RBAC policy with CanPull does not change anything if the user already has pull access
	{Scope: "repository:test1/foo:pull",
		RBACPolicy:     &policyPullMatches,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		RBACPolicy:     &policyPullMatches,
		GrantedActions: "push"},
	{Scope: "repository:test1/foo:pull,push",
		RBACPolicy:     &policyPullMatches,
		GrantedActions: "pull,push"},
	{Scope: "repository:test1/foo:delete",
		RBACPolicy:     &policyPullMatches,
		GrantedActions: "delete"},
	// RBAC policy with CanPull/CanPush grants pull/push permissions to matching users
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyPushMatches,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyPushMatches,
		GrantedActions: "push"},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyPushMatches,
		GrantedActions: "pull,push"},
	{Scope: "repository:test1/foo:delete",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyPushMatches,
		GrantedActions: ""},
	// RBAC policy with CanPull/CanPush does not grant permissions if it does not match
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyPushDoesNotMatch,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyPushDoesNotMatch,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyPushDoesNotMatch,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:delete",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyPushDoesNotMatch,
		GrantedActions: ""},
	// RBAC policy with CanPull/CanPush does not change anything if the user already has pull/push access
	{Scope: "repository:test1/foo:pull",
		RBACPolicy:     &policyPushMatches,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		RBACPolicy:     &policyPushMatches,
		GrantedActions: "push"},
	{Scope: "repository:test1/foo:pull,push",
		RBACPolicy:     &policyPushMatches,
		GrantedActions: "pull,push"},
	{Scope: "repository:test1/foo:delete",
		RBACPolicy:     &policyPushMatches,
		GrantedActions: "delete"},
	// RBAC policy with CanPull/CanDelete grants pull/delete permissions to matching users
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyDeleteMatches,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyDeleteMatches,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyDeleteMatches,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:delete",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyDeleteMatches,
		GrantedActions: "delete"},
	// RBAC policy with CanPull/CanDelete does not grant permissions if it does not match
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyDeleteDoesNotMatch,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyDeleteDoesNotMatch,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyDeleteDoesNotMatch,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:delete",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     &policyDeleteDoesNotMatch,
		GrantedActions: ""},
	// RBAC policy with CanPull/CanDelete does not change anything if the user already has pull/push access
	{Scope: "repository:test1/foo:pull",
		RBACPolicy:     &policyDeleteMatches,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		RBACPolicy:     &policyDeleteMatches,
		GrantedActions: "push"},
	{Scope: "repository:test1/foo:pull,push",
		RBACPolicy:     &policyDeleteMatches,
		GrantedActions: "pull,push"},
	{Scope: "repository:test1/foo:delete",
		RBACPolicy:     &policyDeleteMatches,
		GrantedActions: "delete"},
	// negative RBAC policies can take away permissions
	{Scope: "repository:test1/foo:pull",
		RBACPolicy:     &policyForbidPush,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		RBACPolicy:     &policyForbidPush,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		RBACPolicy:     &policyForbidPush,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:delete",
		RBACPolicy:     &policyForbidPush,
		GrantedActions: "delete"},
}

func BenchmarkParseRBACPoliciesField(b *testing.B) {
	b.ReportAllocs()

	payloads := [][]byte{nil, []byte("[]")}
	for _, tc := range testCases {
		if tc.RBACPolicy == nil {
			continue
		}
		buf := must.Return(json.Marshal([]keppel.RBACPolicy{*tc.RBACPolicy}))
		payloads = append(payloads, buf)
	}

	var err error

	b.ResetTimer()
	for i := range b.N {
		_, err = keppel.ParseRBACPoliciesField(payloads[i%len(payloads)])
		if err != nil {
			b.Fatal(err)
		}
	}
}

// TODO expect refresh_token when offline_token=true is given

func setupPrimary(t *testing.T, extraOptions ...test.SetupOption) test.Setup {
	s := test.NewSetup(t,
		append(extraOptions,
			test.WithAnycast(true),
			test.WithAccount(models.Account{Name: "test1", AuthTenantID: "test1authtenant"}),
		)...,
	)
	s.AD.ExpectedUserName = "correctusername"
	s.AD.ExpectedPassword = "correctpassword"
	return s
}

func setupSecondary(t *testing.T) test.Setup {
	s := test.NewSetup(t,
		test.IsSecondaryTo(nil),
		test.WithAnycast(true),
		test.WithAccount(models.Account{Name: "test2", AuthTenantID: "test1authtenant"}),
	)
	s.AD.ExpectedUserName = "correctusername"
	s.AD.ExpectedPassword = "correctpassword"
	return s
}

// jwtAccess appears in type jwtContents.
type jwtAccess struct {
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	Actions []string `json:"actions"`
}

// jwtContents contains what we expect in a JWT token payload section.
type jwtContents struct {
	Issuer   string
	Subject  string
	Audience string
	Access   []jwtAccess
}

// WithinResponseBody can be used with httptest.Response.Expect() to inspect the token response from GET /keppel/v1/auth.
func (c jwtContents) WithinResponseBody(t *testing.T, status int) func(httptest.Response) {
	return func(resp httptest.Response) {
		t.Helper()
		resp.ExpectStatus(t, status)

		var responseBody struct {
			Token string `json:"token"`
			// optional fields (all listed so that we can use DisallowUnknownFields())
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    uint64 `json:"expires_in"`
			IssuedAt     string `json:"issued_at"`
		}
		responseBodyBytes := resp.BodyBytes()
		dec := json.NewDecoder(bytes.NewReader(responseBodyBytes))
		dec.DisallowUnknownFields()
		err := dec.Decode(&responseBody)
		if err != nil {
			t.Logf("token was: %s", string(responseBodyBytes))
			t.Errorf("cannot decode response body: %s", err.Error())
			return
		}

		// extract payload from token
		tokenFields := strings.Split(responseBody.Token, ".")
		if len(tokenFields) != 3 {
			t.Errorf("expected token with 3 parts, got %d parts (full JWT is %q)", len(tokenFields), responseBody.Token)
			return
		}
		tokenBytes, err := base64.RawURLEncoding.DecodeString(tokenFields[1])
		if err != nil {
			t.Errorf("cannot decode JWT payload section: %s (full JWT is %q)", err.Error(), responseBody.Token)
			return
		}

		// decode token
		var (
			actualExpiresAt int64
			actualNotBefore int64
			actualIssuedAt  int64
		)
		expectedToken := jsonmatch.Object{
			"iss":    c.Issuer,
			"aud":    c.Audience,
			"exp":    jsonmatch.CaptureField(&actualExpiresAt),
			"nbf":    jsonmatch.CaptureField(&actualNotBefore),
			"iat":    jsonmatch.CaptureField(&actualIssuedAt),
			"jti":    jsonmatch.Irrelevant(), // jti = JWT token ID
			"access": c.Access,
			"kea":    jsonmatch.Irrelevant(), // kea = keppel embedded authorization
		}
		if c.Subject != "" {
			expectedToken["sub"] = c.Subject
		}
		hasDiff := false
		for _, diff := range expectedToken.DiffAgainst(tokenBytes) {
			t.Error(diff.String())
			hasDiff = true
		}
		if hasDiff {
			return
		}

		// check remaining token attributes for plausibility
		nowUnix := time.Now().Unix()
		if nowUnix >= actualExpiresAt {
			t.Errorf("ExpiresAt should be in the future, but is %d seconds in the past", nowUnix-actualExpiresAt)
		}
		if nowUnix < actualNotBefore {
			t.Errorf("NotBefore should be now or in the past, but is %d seconds in the future", actualNotBefore-nowUnix)
		}
		if nowUnix < actualIssuedAt {
			t.Errorf("IssuedAt should be now or in the past, but is %d seconds in the future", actualIssuedAt-nowUnix)
		}
	}
}

func TestIssueToken(t *testing.T) {
	s := setupPrimary(t)
	service := s.Config.APIPublicHostname

	for idx, c := range testCases {
		t.Run(fmt.Sprintf("testcase=%d/%d", idx+1, len(testCases)), func(t *testing.T) {
			ctx := t.Context()

			// setup RBAC policies for test
			rbacPoliciesJSONStr := ""
			if c.RBACPolicy != nil {
				buf := must.ReturnT(json.Marshal([]keppel.RBACPolicy{*c.RBACPolicy}))(t)
				rbacPoliciesJSONStr = string(buf)
			}
			test.MustExec(t, s.DB, `UPDATE accounts SET rbac_policies_json = $1 WHERE name = $2`, rbacPoliciesJSONStr, "test1")

			// setup permissions for test
			var perms []string
			if c.CannotDelete {
				perms = append(perms, string(keppel.CanDeleteFromAccount)+":othertenant")
			} else {
				perms = append(perms, string(keppel.CanDeleteFromAccount)+":test1authtenant")
			}
			if c.CannotPush {
				perms = append(perms, string(keppel.CanPushToAccount)+":othertenant")
			} else {
				perms = append(perms, string(keppel.CanPushToAccount)+":test1authtenant")
			}
			if c.CannotPull {
				perms = append(perms, string(keppel.CanPullFromAccount)+":othertenant")
				perms = append(perms, string(keppel.CanViewAccount)+":othertenant")
			} else {
				perms = append(perms, string(keppel.CanPullFromAccount)+":test1authtenant")
				perms = append(perms, string(keppel.CanViewAccount)+":test1authtenant")
			}
			s.AD.GrantedPermissions = strings.Join(perms, ",")

			// build URL query string for test
			query := url.Values{}
			if service != "" {
				query.Set("service", service)
			}
			if c.Scope != "" {
				query.Set("scope", c.Scope)
			}
			path := "/keppel/v1/auth?" + query.Encode()

			// build expected tokenContents to match against
			expectedContents := jwtContents{
				Audience: service,
				Issuer:   "keppel-api@registry.example.org",
				Subject:  "correctusername",
			}
			if c.AnonymousLogin {
				expectedContents.Subject = ""
			}
			if c.GrantedActions != "" {
				fields := strings.SplitN(c.Scope, ":", 3)
				expectedContents.Access = []jwtAccess{{
					Type:    fields[0],
					Name:    fields[1],
					Actions: strings.Split(c.GrantedActions, ","),
				}}
			}
			if len(c.AdditionalScopes) > 0 {
				for _, scope := range c.AdditionalScopes {
					fields := strings.SplitN(scope, ":", 3)
					expectedContents.Access = append(expectedContents.Access, jwtAccess{
						Type:    fields[0],
						Name:    fields[1],
						Actions: strings.Split(fields[2], ","),
					})
				}
			}
			options := []httptest.RequestOption{}
			if !c.AnonymousLogin {
				options = append(options, httptest.WithHeader("Authorization", keppel.BuildBasicAuthHeader("correctusername", "correctpassword")))
			}

			s.RespondTo(ctx, "GET "+path, options...).
				Expect(expectedContents.WithinResponseBody(t, http.StatusOK))
		})
	}
}

func TestInvalidCredentials(t *testing.T) {
	s := setupPrimary(t)
	ctx := t.Context()
	service := s.Config.APIPublicHostname

	// execute normal GET requests that would result in a token with granted
	// actions, if we didn't give the wrong username (in the first call) or
	// password (in the second call)
	urlPath := url.URL{
		Path: "/keppel/v1/auth",
		RawQuery: url.Values{
			"service": {service},
			"scope":   {"repository:test1/foo:pull"},
		}.Encode(),
	}
	reqPath := urlPath.String()
	respondTo := func(authHeader, expectedDetails string) {
		s.RespondTo(ctx, "GET "+reqPath,
			httptest.WithHeader("Authorization", authHeader),
		).ExpectJSON(t, http.StatusUnauthorized, jsonmatch.Object{"details": expectedDetails})
	}

	t.Logf("----- test malformed credentials with service %q -----\n", service)
	respondTo("Bogus 65082567y295847y62", "malformed Authorization header")
	respondTo("Basic 65082567y2958)*&@@", "malformed Authorization header")
	respondTo("Basic "+base64.StdEncoding.EncodeToString([]byte("onlyusername")), "malformed Authorization header")

	t.Logf("----- test wrong username with service %q -----\n", service)
	respondTo(keppel.BuildBasicAuthHeader("wrongusername", "correctpassword"), "wrong credentials")

	t.Logf("----- test wrong password with service %q -----\n", service)
	respondTo(keppel.BuildBasicAuthHeader("correctusername", "wrongpassword"), "wrong credentials")
}

type anycastTestCase struct {
	// request
	AccountName models.AccountName
	Service     string
	Handler     http.Handler
	// result
	ErrorMessage string
	HasAccess    bool
	Issuer       string
}

func TestAnycastAndDomainRemappedTokens(t *testing.T) {
	test.WithRoundTripper(func(tt *test.RoundTripper) {
		ctx := t.Context()
		s1 := setupPrimary(t)
		s2 := setupSecondary(t)
		h1 := s1.Handler
		h2 := s2.Handler

		// setup permissions for test
		perms := fmt.Sprintf("%s:test1authtenant,%s:test1authtenant", keppel.CanPullFromAccount, keppel.CanViewAccount)
		s1.AD.GrantedPermissions = perms
		s2.AD.GrantedPermissions = perms

		localService1 := s1.Config.APIPublicHostname
		localService2 := s2.Config.APIPublicHostname
		anycastService := s1.Config.AnycastAPIPublicHostname
		anycastTestCases := []anycastTestCase{
			// when asking for a local token (i.e. not giving the anycast hostname as
			// service), no reverse-proxying is done and we only see the local accounts
			{AccountName: "test1", Service: localService1, Handler: h1,
				HasAccess: true, Issuer: localService1},
			{AccountName: "test2", Service: localService1, Handler: h1,
				HasAccess: false, Issuer: localService1},
			{AccountName: "test1", Service: localService2, Handler: h2,
				HasAccess: false, Issuer: localService2},
			{AccountName: "test2", Service: localService2, Handler: h2,
				HasAccess: true, Issuer: localService2},
			// asking for a token for someone else's local service will never work
			{AccountName: "test1", Service: localService2, Handler: h1,
				ErrorMessage: `cannot issue tokens for service: "%SERVICE%"`},
			{AccountName: "test2", Service: localService2, Handler: h1,
				ErrorMessage: `cannot issue tokens for service: "%SERVICE%"`},
			{AccountName: "test1", Service: localService1, Handler: h2,
				ErrorMessage: `cannot issue tokens for service: "%SERVICE%"`},
			{AccountName: "test2", Service: localService1, Handler: h2,
				ErrorMessage: `cannot issue tokens for service: "%SERVICE%"`},
			// when asking for an anycast token, the request if reverse-proxied if
			// necessary and we will see the Keppel hosting the primary account as
			// issuer
			{AccountName: "test1", Service: anycastService, Handler: h1,
				HasAccess: true, Issuer: localService1},
			{AccountName: "test2", Service: anycastService, Handler: h1,
				HasAccess: true, Issuer: localService2},
			{AccountName: "test1", Service: anycastService, Handler: h2,
				HasAccess: true, Issuer: localService1},
			{AccountName: "test2", Service: anycastService, Handler: h2,
				HasAccess: true, Issuer: localService2},
			// asking for a token for an account that doesn't exist will never work
			{AccountName: "test3", Service: localService1, Handler: h1,
				HasAccess: false, Issuer: localService1},
			{AccountName: "test3", Service: localService2, Handler: h2,
				HasAccess: false, Issuer: localService2},
			{AccountName: "test3", Service: anycastService, Handler: h1,
				HasAccess: false, Issuer: localService1},
			{AccountName: "test3", Service: anycastService, Handler: h2,
				HasAccess: false, Issuer: localService2},
		}

		correctAuthHeader := keppel.BuildBasicAuthHeader("correctusername", "correctpassword")

		for idx, c := range anycastTestCases {
			for _, withDomainRemapping := range []bool{false, true} {
				t.Logf("----- testcase %d/%d with domain remapping: %t -----\n", idx+1, len(anycastTestCases), withDomainRemapping)

				var (
					domainPrefix  string
					scopeRepoName string
				)
				if withDomainRemapping {
					domainPrefix = string(c.AccountName) + "."
					scopeRepoName = "foo"
				} else {
					domainPrefix = ""
					scopeRepoName = string(c.AccountName) + "/foo"
				}

				path := fmt.Sprintf("/keppel/v1/auth?scope=repository:%s:pull&service=%s%s", scopeRepoName, domainPrefix, c.Service)
				resp := httptest.NewHandler(c.Handler).RespondTo(ctx, "GET "+path,
					httptest.WithHeader("Authorization", correctAuthHeader),
				)

				if c.ErrorMessage == "" {
					// build jwtContents struct to contain issued token against
					expectedContents := jwtContents{
						Audience: domainPrefix + c.Service,
						Issuer:   "keppel-api@" + domainPrefix + c.Issuer,
						Subject:  "correctusername",
					}
					if c.HasAccess {
						expectedContents.Access = []jwtAccess{{
							Type:    "repository",
							Name:    scopeRepoName,
							Actions: []string{"pull"},
						}}
					}
					resp.Expect(expectedContents.WithinResponseBody(t, http.StatusOK))
				} else {
					msg := strings.ReplaceAll(c.ErrorMessage, "%SERVICE%", domainPrefix+c.Service)
					resp.ExpectJSON(t, http.StatusBadRequest, jsonmatch.Object{"details": msg})
				}
			}
		}

		// test that catalog access is not allowed on anycast (since we don't know
		// which peer to ask for authentication)
		path := fmt.Sprintf("/keppel/v1/auth?service=%s&scope=registry:catalog:*", anycastService)
		expectedContents := jwtContents{
			Audience: anycastService,
			Issuer:   "keppel-api@" + localService1,
			Subject:  "correctusername",
			Access:   nil,
		}
		httptest.NewHandler(h1).RespondTo(ctx, "GET "+path,
			httptest.WithHeader("Authorization", correctAuthHeader),
		).Expect(expectedContents.WithinResponseBody(t, http.StatusOK))

		// test that catalog access is allowed for domain-remapped APIs, but only
		// for the account name specified in the domain
		path = fmt.Sprintf("/keppel/v1/auth?service=test1.%s&scope=registry:catalog:*", localService1)
		expectedContents = jwtContents{
			Audience: "test1." + localService1,
			Issuer:   "keppel-api@test1." + localService1,
			Subject:  "correctusername",
			Access: []jwtAccess{
				{Type: "registry", Name: "catalog", Actions: []string{"*"}},
				{Type: "keppel_account", Name: "test1", Actions: []string{"view"}},
			},
		}
		httptest.NewHandler(h1).RespondTo(ctx, "GET "+path,
			httptest.WithHeader("Authorization", correctAuthHeader),
		).Expect(expectedContents.WithinResponseBody(t, http.StatusOK))

		path = fmt.Sprintf("/keppel/v1/auth?service=something-else.%s&scope=registry:catalog:*", localService1)
		expectedContents = jwtContents{
			Audience: "something-else." + localService1,
			Issuer:   "keppel-api@something-else." + localService1,
			Subject:  "correctusername",
			Access: []jwtAccess{
				{Type: "registry", Name: "catalog", Actions: []string{"*"}},
				// no keppel_account:test1:view since the API is restricted to the non-existent account "something-else"
			},
		}
		httptest.NewHandler(h1).RespondTo(ctx, "GET "+path,
			httptest.WithHeader("Authorization", correctAuthHeader),
		).Expect(expectedContents.WithinResponseBody(t, http.StatusOK))
	})
}

func TestMultiScope(t *testing.T) {
	// It turns out that it's allowed to send multiple scopes in a single auth
	// request, which produces a token with a union of all granted scopes. This
	// test covers some basic cases of multi-scopes.

	s := setupPrimary(t)
	ctx := t.Context()
	service := s.Config.APIPublicHostname

	// various shorthands for the testcases below
	correctAuthHeader := keppel.BuildBasicAuthHeader("correctusername", "correctpassword")
	makeJWTContents := func(access []jwtAccess) jwtContents {
		return jwtContents{
			Audience: service,
			Issuer:   "keppel-api@" + service,
			Subject:  "correctusername",
			Access:   access,
		}
	}
	makePerms := func(perms ...keppel.Permission) string {
		var fields []string
		for _, perm := range perms {
			fields = append(fields, string(perm)+":test1authtenant")
		}
		return strings.Join(fields, ",")
	}

	// case 1: multiple actions on the same resource and we get everything we ask for
	s.AD.GrantedPermissions = makePerms(keppel.CanViewAccount, keppel.CanPullFromAccount, keppel.CanPushToAccount, keppel.CanDeleteFromAccount)
	path := fmt.Sprintf("/keppel/v1/auth?service=%s&scope=repository:test1/foo:pull&scope=repository:test1/foo:push&scope=repository:test1/foo:delete", service)
	expectedContents := makeJWTContents([]jwtAccess{{
		Type:    "repository",
		Name:    "test1/foo",
		Actions: []string{"pull", "push", "delete"},
	}})
	s.RespondTo(ctx, "GET "+path,
		httptest.WithHeader("Authorization", correctAuthHeader),
	).Expect(expectedContents.WithinResponseBody(t, http.StatusOK))

	// case 2: overlapping actions on the same resource and we get everything except "delete"
	s.AD.GrantedPermissions = makePerms(keppel.CanViewAccount, keppel.CanPullFromAccount, keppel.CanPushToAccount)
	path = fmt.Sprintf("/keppel/v1/auth?service=%s&scope=repository:test1/foo:pull,delete&scope=repository:test1/foo:pull,push&scope=repository:test1/foo:delete", service)
	expectedContents = makeJWTContents([]jwtAccess{{
		Type:    "repository",
		Name:    "test1/foo",
		Actions: []string{"pull", "push"}, // "pull" was mentioned twice in the scopes - this verifies that it was deduplicated
	}})
	s.RespondTo(ctx, "GET "+path,
		httptest.WithHeader("Authorization", correctAuthHeader),
	).Expect(expectedContents.WithinResponseBody(t, http.StatusOK))

	// case 3: actions on multiple resources and we reject access to one of the resources entirely
	s.AD.GrantedPermissions = makePerms(keppel.CanViewAccount, keppel.CanPullFromAccount, keppel.CanPushToAccount)
	path = fmt.Sprintf("/keppel/v1/auth?service=%s&scope=repository:test1/foo:pull,push&scope=repository:test2/foo:pull,push&scope=registry:catalog:*", service)
	expectedContents = makeJWTContents([]jwtAccess{
		{
			Type:    "repository",
			Name:    "test1/foo",
			Actions: []string{"pull", "push"},
		},
		{
			Type:    "registry",
			Name:    "catalog",
			Actions: []string{"*"},
		},
		{
			Type:    "keppel_account",
			Name:    "test1",
			Actions: []string{"view"},
		},
	})
	s.RespondTo(ctx, "GET "+path,
		httptest.WithHeader("Authorization", correctAuthHeader),
	).Expect(expectedContents.WithinResponseBody(t, http.StatusOK))
}

func TestIssuerKeyRotation(t *testing.T) {
	ctx := t.Context()

	// phase 1: issue a token with the previous issuer key
	s := setupPrimary(t, test.WithPreviousIssuerKey, test.WithoutCurrentIssuerKey)
	expectedContents := jwtContents{
		Audience: "registry.example.org",
		Issuer:   "keppel-api@registry.example.org",
		Subject:  "correctusername",
		Access: []jwtAccess{{
			Type:    "keppel_api",
			Name:    "info",
			Actions: []string{"access"},
		}},
	}
	resp := s.RespondTo(ctx, "GET /keppel/v1/auth?service=registry.example.org&scope=keppel_api:info:access",
		httptest.WithHeader("Authorization", keppel.BuildBasicAuthHeader("correctusername", "correctpassword")),
	)
	resp.Expect(expectedContents.WithinResponseBody(t, http.StatusOK))

	var respBody struct {
		Token string `json:"token"`
	}
	must.SucceedT(t, json.Unmarshal(resp.BodyBytes(), &respBody))

	// test that it (obviously) gets accepted by the same API that issued it
	s.RespondTo(ctx, "GET /v2/",
		httptest.WithHeader("Authorization", "Bearer "+respBody.Token),
	).ExpectStatus(t, http.StatusOK)

	// phase 2: check that the token still gets accepted when a new key gets rotated in
	s = setupPrimary(t, test.WithPreviousIssuerKey)
	s.RespondTo(ctx, "GET /v2/",
		httptest.WithHeader("Authorization", "Bearer "+respBody.Token),
	).ExpectStatus(t, http.StatusOK)

	// phase 3: check that the token does NOT get accepted anymore when the old key has rotated out
	s = setupPrimary(t)
	s.RespondTo(ctx, "GET /v2/",
		httptest.WithHeader("Authorization", "Bearer "+respBody.Token),
	).ExpectJSON(t, http.StatusUnauthorized, jsonmatch.Object{
		"errors": []jsonmatch.Object{{
			"code":    string(keppel.ErrUnauthorized),
			"message": "token is unverifiable: error while executing keyfunc: token signed by unknown key",
			"detail":  nil,
		}},
	})
}
