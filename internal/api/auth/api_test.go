/******************************************************************************
*
*  Copyright 2019-2020 SAP SE
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

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"

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
		Permissions:       []keppel.RBACPermission{keppel.GrantsAnonymousPull},
	}
	policyAnonFirstPull = keppel.RBACPolicy{
		RepositoryPattern: "fo+",
		Permissions:       []keppel.RBACPermission{keppel.GrantsAnonymousPull, keppel.GrantsAnonymousFirstPull},
	}
	policyPullMatches = keppel.RBACPolicy{
		RepositoryPattern: "fo+",
		UserNamePattern:   "correct.*",
		Permissions:       []keppel.RBACPermission{keppel.GrantsPull},
	}
	policyPushMatches = keppel.RBACPolicy{
		RepositoryPattern: "fo+",
		UserNamePattern:   "correct.*",
		Permissions:       []keppel.RBACPermission{keppel.GrantsPull, keppel.GrantsPush},
	}
	policyDeleteMatches = keppel.RBACPolicy{
		RepositoryPattern: "fo+",
		UserNamePattern:   "correct.*",
		Permissions:       []keppel.RBACPermission{keppel.GrantsPull, keppel.GrantsDelete},
	}
	policyPullDoesNotMatch = keppel.RBACPolicy{
		RepositoryPattern: "fo+",
		UserNamePattern:   "doesnotmatch",
		Permissions:       []keppel.RBACPermission{keppel.GrantsPull},
	}
	policyPushDoesNotMatch = keppel.RBACPolicy{
		RepositoryPattern: "doesnotmatch",
		UserNamePattern:   "correct.*",
		Permissions:       []keppel.RBACPermission{keppel.GrantsPull, keppel.GrantsPush},
	}
	policyDeleteDoesNotMatch = keppel.RBACPolicy{
		RepositoryPattern: "fo+",
		UserNamePattern:   "doesnotmatch",
		Permissions:       []keppel.RBACPermission{keppel.GrantsPull, keppel.GrantsDelete},
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

// jwtAccess appears in type jwtToken.
type jwtAccess struct {
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	Actions []string `json:"actions"`
}

// jwtToken contains the parsed contents of the payload section of a JWT token.
type jwtToken struct {
	Issuer    string      `json:"iss"`
	Subject   string      `json:"sub"`
	Audience  string      `json:"aud"`
	ExpiresAt int64       `json:"exp"`
	NotBefore int64       `json:"nbf"`
	IssuedAt  int64       `json:"iat"`
	TokenID   string      `json:"jti"`
	Access    []jwtAccess `json:"access"`
	// The EmbeddedAuthorization is ignored by this test. It will be exercised
	// indirectly in the registry API tests since the registry API uses attributes
	// from the EmbeddedAuthorization.
	Ignored map[string]any `json:"kea"`
}

// jwtContents contains what we expect in a JWT token payload section. This type
// implements assert.HTTPResponseBody and can therefore be used with
// assert.HTTPRequest.
type jwtContents struct {
	Issuer   string
	Subject  string
	Audience string
	Access   []jwtAccess
}

// AssertResponseBody implements the assert.HTTPResponseBody interface.
func (c jwtContents) AssertResponseBody(t *testing.T, requestInfo string, responseBodyBytes []byte) (ok bool) {
	t.Helper()

	var responseBody struct {
		Token string `json:"token"`
		// optional fields (all listed so that we can use DisallowUnknownFields())
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    uint64 `json:"expires_in"`
		IssuedAt     string `json:"issued_at"`
	}
	dec := json.NewDecoder(bytes.NewReader(responseBodyBytes))
	dec.DisallowUnknownFields()
	err := dec.Decode(&responseBody)
	if err != nil {
		t.Logf("token was: %s", string(responseBodyBytes))
		t.Errorf("%s: cannot decode response body: %s", requestInfo, err.Error())
		return false
	}

	// extract payload from token
	tokenFields := strings.Split(responseBody.Token, ".")
	if len(tokenFields) != 3 {
		t.Logf("JWT is %s", responseBody.Token)
		t.Errorf("%s: expected token with 3 parts, got %d parts", requestInfo, len(tokenFields))
		return false
	}
	tokenBytes, err := base64.RawURLEncoding.DecodeString(tokenFields[1])
	if err != nil {
		t.Logf("JWT is %s", responseBody.Token)
		t.Errorf("%s: cannot decode JWT payload section: %s", requestInfo, err.Error())
		return false
	}

	// decode token
	var token jwtToken
	dec = json.NewDecoder(bytes.NewReader(tokenBytes))
	dec.DisallowUnknownFields()
	err = dec.Decode(&token)
	if err != nil {
		t.Logf("token JSON is %s", string(tokenBytes))
		t.Errorf("%s: cannot deserialize JWT payload section: %s", requestInfo, err.Error())
		return false
	}

	// check token attributes for correctness
	ok = true
	ok = ok && assert.DeepEqual(t, "token.Access for "+requestInfo, token.Access, c.Access)
	ok = ok && assert.DeepEqual(t, "token.Audience for "+requestInfo, token.Audience, c.Audience)
	ok = ok && assert.DeepEqual(t, "token.Issuer for "+requestInfo, token.Issuer, c.Issuer)
	ok = ok && assert.DeepEqual(t, "token.Subject for "+requestInfo, token.Subject, c.Subject)

	// check remaining token attributes for plausibility
	nowUnix := time.Now().Unix()
	if nowUnix >= token.ExpiresAt {
		t.Errorf("%s: ExpiresAt should be in the future, but is %d seconds in the past", requestInfo, nowUnix-token.ExpiresAt)
		ok = false
	}
	if nowUnix < token.NotBefore {
		t.Errorf("%s: NotBefore should be now or in the past, but is %d seconds in the future", requestInfo, token.NotBefore-nowUnix)
		ok = false
	}
	if nowUnix < token.IssuedAt {
		t.Errorf("%s: IssuedAt should be now or in the past, but is %d seconds in the future", requestInfo, token.IssuedAt-nowUnix)
		ok = false
	}

	return ok
}

func TestIssueToken(t *testing.T) {
	s := setupPrimary(t)
	service := s.Config.APIPublicHostname

	for idx, c := range testCases {
		t.Logf("----- testcase %d/%d -----\n", idx+1, len(testCases))

		// setup RBAC policies for test
		rbacPoliciesJSONStr := ""
		if c.RBACPolicy != nil {
			buf, err := json.Marshal([]keppel.RBACPolicy{*c.RBACPolicy})
			if err != nil {
				t.Fatal(err.Error())
			}
			rbacPoliciesJSONStr = string(buf)
		}
		_, err := s.DB.Exec(`UPDATE accounts SET rbac_policies_json = $1 WHERE name = $2`, rbacPoliciesJSONStr, "test1")
		if err != nil {
			t.Fatal(err.Error())
		}

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

		// setup Authorization header for test
		req := assert.HTTPRequest{
			Method:       "GET",
			ExpectStatus: http.StatusOK,
		}
		if !c.AnonymousLogin {
			req.Header = map[string]string{
				"Authorization": keppel.BuildBasicAuthHeader("correctusername", "correctpassword"),
			}
		}

		// build URL query string for test
		query := url.Values{}
		if service != "" {
			query.Set("service", service)
		}
		if c.Scope != "" {
			query.Set("scope", c.Scope)
		}
		req.Path = "/keppel/v1/auth?" + query.Encode()

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
		req.ExpectBody = expectedContents

		// execute request
		req.Check(t, s.Handler)
	}
}

func TestInvalidCredentials(t *testing.T) {
	s := setupPrimary(t)
	h := s.Handler
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
	req := assert.HTTPRequest{
		Method:       "GET",
		Path:         urlPath.String(),
		Header:       map[string]string{},
		ExpectStatus: http.StatusUnauthorized,
		ExpectBody:   assert.JSONObject{"details": "incorrect username or password"},
	}

	t.Logf("----- test malformed credentials with service %q -----\n", service)
	req.Header["Authorization"] = "Bogus 65082567y295847y62"
	req.ExpectBody = assert.JSONObject{"details": "malformed Authorization header"}
	req.Check(t, h)
	req.Header["Authorization"] = "Basic 65082567y2958)*&@@"
	req.Check(t, h)
	req.Header["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte("onlyusername"))
	req.Check(t, h)

	t.Logf("----- test wrong username with service %q -----\n", service)
	req.Header["Authorization"] = keppel.BuildBasicAuthHeader("wrongusername", "correctpassword")
	req.ExpectBody = assert.JSONObject{"details": "wrong credentials"}
	req.Check(t, h)

	t.Logf("----- test wrong password with service %q -----\n", service)
	req.Header["Authorization"] = keppel.BuildBasicAuthHeader("correctusername", "wrongpassword")
	req.Check(t, h)
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

		correctAuthHeader := map[string]string{
			"Authorization": keppel.BuildBasicAuthHeader("correctusername", "correctpassword"),
		}

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

				req := assert.HTTPRequest{
					Method: "GET",
					Path:   fmt.Sprintf("/keppel/v1/auth?scope=repository:%s:pull&service=%s%s", scopeRepoName, domainPrefix, c.Service),
					Header: correctAuthHeader,
				}

				if c.ErrorMessage == "" {
					req.ExpectStatus = http.StatusOK

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
					req.ExpectBody = expectedContents
				} else {
					msg := strings.ReplaceAll(c.ErrorMessage, "%SERVICE%", domainPrefix+c.Service)
					req.ExpectStatus = http.StatusBadRequest
					req.ExpectBody = assert.JSONObject{"details": msg}
				}

				req.Check(t, c.Handler)
			}
		}

		// test that catalog access is not allowed on anycast (since we don't know
		// which peer to ask for authentication)
		assert.HTTPRequest{
			Method:       "GET",
			Path:         fmt.Sprintf("/keppel/v1/auth?service=%s&scope=registry:catalog:*", anycastService),
			Header:       correctAuthHeader,
			ExpectStatus: http.StatusOK,
			ExpectBody: jwtContents{
				Audience: anycastService,
				Issuer:   "keppel-api@" + localService1,
				Subject:  "correctusername",
				Access:   nil,
			},
		}.Check(t, h1)

		// test that catalog access is allowed for domain-remapped APIs, but only
		// for the account name specified in the domain
		assert.HTTPRequest{
			Method:       "GET",
			Path:         fmt.Sprintf("/keppel/v1/auth?service=test1.%s&scope=registry:catalog:*", localService1),
			Header:       correctAuthHeader,
			ExpectStatus: http.StatusOK,
			ExpectBody: jwtContents{
				Audience: "test1." + localService1,
				Issuer:   "keppel-api@test1." + localService1,
				Subject:  "correctusername",
				Access: []jwtAccess{
					{Type: "registry", Name: "catalog", Actions: []string{"*"}},
					{Type: "keppel_account", Name: "test1", Actions: []string{"view"}},
				},
			},
		}.Check(t, h1)
		assert.HTTPRequest{
			Method:       "GET",
			Path:         fmt.Sprintf("/keppel/v1/auth?service=something-else.%s&scope=registry:catalog:*", localService1),
			Header:       correctAuthHeader,
			ExpectStatus: http.StatusOK,
			ExpectBody: jwtContents{
				Audience: "something-else." + localService1,
				Issuer:   "keppel-api@something-else." + localService1,
				Subject:  "correctusername",
				Access: []jwtAccess{
					{Type: "registry", Name: "catalog", Actions: []string{"*"}},
					// no keppel_account:test1:view since the API is restricted to the non-existent account "something-else"
				},
			},
		}.Check(t, h1)
	})
}

func TestMultiScope(t *testing.T) {
	// It turns out that it's allowed to send multiple scopes in a single auth
	// request, which produces a token with a union of all granted scopes. This
	// test covers some basic cases of multi-scopes.

	s := setupPrimary(t)
	h := s.Handler
	service := s.Config.APIPublicHostname

	// various shorthands for the testcases below
	correctAuthHeader := map[string]string{
		"Authorization": keppel.BuildBasicAuthHeader("correctusername", "correctpassword"),
	}
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
	assert.HTTPRequest{
		Method:       "GET",
		Path:         fmt.Sprintf("/keppel/v1/auth?service=%s&scope=repository:test1/foo:pull&scope=repository:test1/foo:push&scope=repository:test1/foo:delete", service),
		Header:       correctAuthHeader,
		ExpectStatus: http.StatusOK,
		ExpectBody: makeJWTContents([]jwtAccess{{
			Type:    "repository",
			Name:    "test1/foo",
			Actions: []string{"pull", "push", "delete"},
		}}),
	}.Check(t, h)

	// case 2: overlapping actions on the same resource and we get everything except "delete"
	s.AD.GrantedPermissions = makePerms(keppel.CanViewAccount, keppel.CanPullFromAccount, keppel.CanPushToAccount)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         fmt.Sprintf("/keppel/v1/auth?service=%s&scope=repository:test1/foo:pull,delete&scope=repository:test1/foo:pull,push&scope=repository:test1/foo:delete", service),
		Header:       correctAuthHeader,
		ExpectStatus: http.StatusOK,
		ExpectBody: makeJWTContents([]jwtAccess{{
			Type:    "repository",
			Name:    "test1/foo",
			Actions: []string{"pull", "push"}, // "pull" was mentioned twice in the scopes - this verifies that it was deduplicated
		}}),
	}.Check(t, h)

	// case 3: actions on multiple resources and we reject access to one of the resources entirely
	s.AD.GrantedPermissions = makePerms(keppel.CanViewAccount, keppel.CanPullFromAccount, keppel.CanPushToAccount)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         fmt.Sprintf("/keppel/v1/auth?service=%s&scope=repository:test1/foo:pull,push&scope=repository:test2/foo:pull,push&scope=registry:catalog:*", service),
		Header:       correctAuthHeader,
		ExpectStatus: http.StatusOK,
		ExpectBody: makeJWTContents([]jwtAccess{
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
		}),
	}.Check(t, h)
}

func TestIssuerKeyRotation(t *testing.T) {
	// phase 1: issue a token with the previous issuer key
	s := setupPrimary(t, test.WithPreviousIssuerKey, test.WithoutCurrentIssuerKey)
	_, respBodyBytes := assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/auth?service=registry.example.org&scope=keppel_api:info:access",
		Header:       map[string]string{"Authorization": keppel.BuildBasicAuthHeader("correctusername", "correctpassword")},
		ExpectStatus: http.StatusOK,
		ExpectBody: jwtContents{
			Audience: "registry.example.org",
			Issuer:   "keppel-api@registry.example.org",
			Subject:  "correctusername",
			Access: []jwtAccess{{
				Type:    "keppel_api",
				Name:    "info",
				Actions: []string{"access"},
			}},
		},
	}.Check(t, s.Handler)
	var respBody struct {
		Token string `json:"token"`
	}
	err := json.Unmarshal(respBodyBytes, &respBody)
	if err != nil {
		t.Fatal(err.Error())
	}

	// test that it (obviously) gets accepted by the same API that issued it
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/",
		Header:       map[string]string{"Authorization": "Bearer " + respBody.Token},
		ExpectStatus: http.StatusOK,
	}.Check(t, s.Handler)

	// phase 2: check that the token still gets accepted when a new key gets rotated in
	s = setupPrimary(t, test.WithPreviousIssuerKey)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/",
		Header:       map[string]string{"Authorization": "Bearer " + respBody.Token},
		ExpectStatus: http.StatusOK,
	}.Check(t, s.Handler)

	// phase 3: check that the token does NOT get accepted anymore when the old key has rotated out
	s = setupPrimary(t)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/",
		Header:       map[string]string{"Authorization": "Bearer " + respBody.Token},
		ExpectStatus: http.StatusUnauthorized,
		ExpectBody: assert.JSONObject{
			"errors": []assert.JSONObject{{
				"code":    string(keppel.ErrUnauthorized),
				"message": "token is unverifiable: error while executing keyfunc: token signed by unknown key",
				"detail":  nil,
			}},
		},
	}.Check(t, s.Handler)
}
