/******************************************************************************
*
*  Copyright 2019 SAP SE
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

package authapi

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
	"github.com/sapcc/keppel/internal/api"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

////////////////////////////////////////////////////////////////////////////////
// The testcases in this file encode a lot of knowledge that I gained by
// torturing the auth API of Docker Hub. DO NOT CHANGE stuff unless you have
// verified how the Docker Hub auth endpoint works.
// For the record, the auth endpoint of Docker Hub can be found by
//
//     curl -si https://registry-1.docker.io/v2/ | grep Authenticate

type TestCase struct {
	//request
	Scope          string
	AnonymousLogin bool
	//situation
	CannotPush   bool
	CannotPull   bool
	CannotDelete bool
	RBACPolicy   keppel.RBACPolicy
	//result
	GrantedActions   string
	AdditionalScopes []string
}

var (
	policyAnonPull = keppel.RBACPolicy{
		RepositoryPattern:  "fo+",
		CanPullAnonymously: true,
	}
	policyPullMatches = keppel.RBACPolicy{
		RepositoryPattern: "fo+",
		UserNamePattern:   "correct.*",
		CanPull:           true,
	}
	policyPushMatches = keppel.RBACPolicy{
		RepositoryPattern: "fo+",
		UserNamePattern:   "correct.*",
		CanPull:           true,
		CanPush:           true,
	}
	policyDeleteMatches = keppel.RBACPolicy{
		RepositoryPattern: "fo+",
		UserNamePattern:   "correct.*",
		CanPull:           true,
		CanDelete:         true,
	}
	policyPullDoesNotMatch = keppel.RBACPolicy{
		RepositoryPattern: "fo+",
		UserNamePattern:   "doesnotmatch",
		CanPull:           true,
	}
	policyPushDoesNotMatch = keppel.RBACPolicy{
		RepositoryPattern: "doesnotmatch",
		UserNamePattern:   "correct.*",
		CanPull:           true,
		CanPush:           true,
	}
	policyDeleteDoesNotMatch = keppel.RBACPolicy{
		RepositoryPattern: "fo+",
		UserNamePattern:   "doesnotmatch",
		CanPull:           true,
		CanDelete:         true,
	}
)

var testCases = []TestCase{
	//basic success case
	{Scope: "repository:test1/foo:pull",
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		GrantedActions: "push"},
	{Scope: "repository:test1/foo:pull,push",
		GrantedActions: "pull,push"},
	{Scope: "repository:test1/foo:delete",
		GrantedActions: "delete"},
	//not allowed to pull
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, GrantedActions: ""},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, GrantedActions: "push"},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, GrantedActions: "push"},
	{Scope: "repository:test1/foo:delete",
		CannotPull: true, GrantedActions: "delete"},
	//not allowed to push
	{Scope: "repository:test1/foo:pull",
		CannotPush: true, GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		CannotPush: true, GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		CannotPush: true, GrantedActions: "pull"},
	{Scope: "repository:test1/foo:delete",
		CannotPush: true, GrantedActions: "delete"},
	//not allowed to pull nor push
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, CannotPush: true, GrantedActions: ""},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, CannotPush: true, GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, CannotPush: true, GrantedActions: ""},
	{Scope: "repository:test1/foo:delete",
		CannotPull: true, CannotPush: true, GrantedActions: "delete"},
	//not allowed to delete
	{Scope: "repository:test1/foo:pull",
		CannotDelete: true, GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		CannotDelete: true, GrantedActions: "push"},
	{Scope: "repository:test1/foo:pull,push",
		CannotDelete: true, GrantedActions: "pull,push"},
	{Scope: "repository:test1/foo:delete",
		CannotDelete: true, GrantedActions: ""},
	//catalog access always allowed if username/password are ok (access to
	//specific accounts is filtered later)
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
	//unknown resources/actions for resource type "registry"
	{Scope: "registry:test1/foo:pull",
		GrantedActions: ""},
	{Scope: "registry:catalog:pull",
		GrantedActions: ""},
	//incomplete scope syntax
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
	//invalid scope syntax (overlong repository name)
	{Scope: fmt.Sprintf("repository:test1/%s:pull", strings.Repeat("a", 300)),
		GrantedActions: ""},
	//invalid scope syntax (malformed repository name)
	{Scope: "repository:test1/???:pull",
		GrantedActions: ""},
	//anonymous login when RBAC policies do not allow access
	{Scope: "repository:test1/foo:pull", AnonymousLogin: true,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:push", AnonymousLogin: true,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push", AnonymousLogin: true,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:delete", AnonymousLogin: true,
		GrantedActions: ""},
	//anonymous pull (but not push) is allowed by a matching RBAC policy
	{Scope: "repository:test1/foo:pull", AnonymousLogin: true,
		RBACPolicy:     policyAnonPull,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push", AnonymousLogin: true,
		RBACPolicy:     policyAnonPull,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push", AnonymousLogin: true,
		RBACPolicy:     policyAnonPull,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:delete", AnonymousLogin: true,
		RBACPolicy:     policyAnonPull,
		GrantedActions: ""},
	//RBAC policy with RepositoryPattern only works when repository name matches
	{Scope: "repository:test1/foobar:pull", AnonymousLogin: true,
		RBACPolicy:     policyAnonPull,
		GrantedActions: ""},
	{Scope: "repository:test1/foobar:push", AnonymousLogin: true,
		RBACPolicy:     policyAnonPull,
		GrantedActions: ""},
	{Scope: "repository:test1/foobar:pull,push", AnonymousLogin: true,
		RBACPolicy:     policyAnonPull,
		GrantedActions: ""},
	{Scope: "repository:test1/foobar:delete", AnonymousLogin: true,
		RBACPolicy:     policyAnonPull,
		GrantedActions: ""},
	//RBAC policy for anonymous pull also enables pull access for all authenticated users
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyAnonPull,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyAnonPull,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyAnonPull,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:delete",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyAnonPull,
		GrantedActions: ""},
	//RBAC policy for anonymous pull does not change anything if the user already has pull access
	{Scope: "repository:test1/foo:pull",
		RBACPolicy:     policyAnonPull,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		RBACPolicy:     policyAnonPull,
		GrantedActions: "push"},
	{Scope: "repository:test1/foo:pull,push",
		RBACPolicy:     policyAnonPull,
		GrantedActions: "pull,push"},
	{Scope: "repository:test1/foo:delete",
		RBACPolicy:     policyAnonPull,
		GrantedActions: "delete"},
	//RBAC policy with CanPull grants pull permissions to matching users
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyPullMatches,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyPullMatches,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyPullMatches,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:delete",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyPullMatches,
		GrantedActions: ""},
	//RBAC policy with CanPull does not grant permissions if it does not match
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyPullDoesNotMatch,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyPullDoesNotMatch,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyPullDoesNotMatch,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:delete",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyPullDoesNotMatch,
		GrantedActions: ""},
	//RBAC policy with CanPull does not change anything if the user already has pull access
	{Scope: "repository:test1/foo:pull",
		RBACPolicy:     policyPullMatches,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		RBACPolicy:     policyPullMatches,
		GrantedActions: "push"},
	{Scope: "repository:test1/foo:pull,push",
		RBACPolicy:     policyPullMatches,
		GrantedActions: "pull,push"},
	{Scope: "repository:test1/foo:delete",
		RBACPolicy:     policyPullMatches,
		GrantedActions: "delete"},
	//RBAC policy with CanPull/CanPush grants pull/push permissions to matching users
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyPushMatches,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyPushMatches,
		GrantedActions: "push"},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyPushMatches,
		GrantedActions: "pull,push"},
	{Scope: "repository:test1/foo:delete",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyPushMatches,
		GrantedActions: ""},
	//RBAC policy with CanPull/CanPush does not grant permissions if it does not match
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyPushDoesNotMatch,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyPushDoesNotMatch,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyPushDoesNotMatch,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:delete",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyPushDoesNotMatch,
		GrantedActions: ""},
	//RBAC policy with CanPull/CanPush does not change anything if the user already has pull/push access
	{Scope: "repository:test1/foo:pull",
		RBACPolicy:     policyPushMatches,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		RBACPolicy:     policyPushMatches,
		GrantedActions: "push"},
	{Scope: "repository:test1/foo:pull,push",
		RBACPolicy:     policyPushMatches,
		GrantedActions: "pull,push"},
	{Scope: "repository:test1/foo:delete",
		RBACPolicy:     policyPushMatches,
		GrantedActions: "delete"},
	//RBAC policy with CanPull/CanDelete grants pull/delete permissions to matching users
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyDeleteMatches,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyDeleteMatches,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyDeleteMatches,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:delete",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyDeleteMatches,
		GrantedActions: "delete"},
	//RBAC policy with CanPull/CanDelete does not grant permissions if it does not match
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyDeleteDoesNotMatch,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyDeleteDoesNotMatch,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyDeleteDoesNotMatch,
		GrantedActions: ""},
	{Scope: "repository:test1/foo:delete",
		CannotPull: true, CannotPush: true, CannotDelete: true,
		RBACPolicy:     policyDeleteDoesNotMatch,
		GrantedActions: ""},
	//RBAC policy with CanPull/CanDelete does not change anything if the user already has pull/push access
	{Scope: "repository:test1/foo:pull",
		RBACPolicy:     policyDeleteMatches,
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		RBACPolicy:     policyDeleteMatches,
		GrantedActions: "push"},
	{Scope: "repository:test1/foo:pull,push",
		RBACPolicy:     policyDeleteMatches,
		GrantedActions: "pull,push"},
	{Scope: "repository:test1/foo:delete",
		RBACPolicy:     policyDeleteMatches,
		GrantedActions: "delete"},
}

//TODO expect refresh_token when offline_token=true is given

func setupPrimary(t *testing.T) (http.Handler, keppel.Configuration, *test.AuthDriver, *test.FederationDriver, *keppel.DB) {
	return setupGeneric(t, false, keppel.Account{
		Name:         "test1",
		AuthTenantID: "test1authtenant",
	})
}

func setupSecondary(t *testing.T) (http.Handler, keppel.Configuration, *test.AuthDriver, *test.FederationDriver, *keppel.DB) {
	return setupGeneric(t, true, keppel.Account{
		Name:         "test2",
		AuthTenantID: "test1authtenant",
	})
}

func setupGeneric(t *testing.T, isSecondary bool, testAccount keppel.Account) (http.Handler, keppel.Configuration, *test.AuthDriver, *test.FederationDriver, *keppel.DB) {
	cfg, db := test.Setup(t, &test.SetupOptions{
		IsSecondary: isSecondary,
		WithAnycast: true,
	})

	//set up a dummy account for testing
	err := db.Insert(&testAccount)
	if err != nil {
		t.Fatal(err.Error())
	}

	adGeneric, err := keppel.NewAuthDriver("unittest", nil)
	if err != nil {
		t.Fatal(err.Error())
	}
	ad := adGeneric.(*test.AuthDriver)
	ad.ExpectedUserName = "correctusername"
	ad.ExpectedPassword = "correctpassword"

	fd, err := keppel.NewFederationDriver("unittest", ad, cfg)
	if err != nil {
		t.Fatal(err.Error())
	}
	fd.RecordExistingAccount(testAccount, time.Unix(0, 0))

	h := api.Compose(NewAPI(cfg, ad, fd, db))
	return h, cfg, ad, fd.(*test.FederationDriver), db
}

//jwtAccess appears in type jwtToken.
type jwtAccess struct {
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	Actions []string `json:"actions"`
}

//jwtToken contains the parsed contents of the payload section of a JWT token.
type jwtToken struct {
	Issuer    string      `json:"iss"`
	Subject   string      `json:"sub"`
	Audience  string      `json:"aud"`
	ExpiresAt int64       `json:"exp"`
	NotBefore int64       `json:"nbf"`
	IssuedAt  int64       `json:"iat"`
	TokenID   string      `json:"jti"`
	Access    []jwtAccess `json:"access"`
}

//jwtContents contains what we expect in a JWT token payload section. This type
//implements assert.HTTPResponseBody and can therefore be used with
//assert.HTTPRequest.
type jwtContents struct {
	Issuer   string
	Subject  string
	Audience string
	Access   []jwtAccess
}

//AssertResponseBody implements the assert.HTTPResponseBody interface.
func (c jwtContents) AssertResponseBody(t *testing.T, requestInfo string, responseBodyBytes []byte) (ok bool) {
	var responseBody struct {
		Token string `json:"token"`
		//optional fields (all listed so that we can use DisallowUnknownFields())
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

	//extract payload from token
	tokenFields := strings.Split(responseBody.Token, ".")
	if len(tokenFields) != 3 {
		t.Logf("JWT is %s", string(responseBody.Token))
		t.Errorf("%s: expected token with 3 parts, got %d parts", requestInfo, len(tokenFields))
		return false
	}
	tokenBytes, err := base64.RawURLEncoding.DecodeString(tokenFields[1])
	if err != nil {
		t.Logf("JWT is %s", string(responseBody.Token))
		t.Errorf("%s: cannot decode JWT payload section: %s", requestInfo, err.Error())
		return false
	}

	//decode token
	var token jwtToken
	dec = json.NewDecoder(bytes.NewReader(tokenBytes))
	dec.DisallowUnknownFields()
	err = dec.Decode(&token)
	if err != nil {
		t.Logf("token JSON is %s", string(tokenBytes))
		t.Errorf("%s: cannot deserialize JWT payload section: %s", requestInfo, err.Error())
		return false
	}

	//check token attributes for correctness
	ok = true
	ok = ok && assert.DeepEqual(t, "token.Access for "+requestInfo, token.Access, c.Access)
	ok = ok && assert.DeepEqual(t, "token.Audience for "+requestInfo, token.Audience, c.Audience)
	ok = ok && assert.DeepEqual(t, "token.Issuer for "+requestInfo, token.Issuer, c.Issuer)
	ok = ok && assert.DeepEqual(t, "token.Subject for "+requestInfo, token.Subject, c.Subject)

	//check remaining token attributes for plausibility
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
	r, cfg, ad, _, db := setupPrimary(t)
	service := cfg.APIPublicURL.Hostname()

	for idx, c := range testCases {
		t.Logf("----- testcase %d/%d -----\n", idx+1, len(testCases))

		//setup RBAC policies for test
		_, err := db.Exec(`DELETE FROM rbac_policies WHERE account_name = $1`, "test1")
		if err != nil {
			t.Fatal(err.Error())
		}
		if c.RBACPolicy != (keppel.RBACPolicy{}) {
			policy := c.RBACPolicy //take a clone for modifying
			policy.AccountName = "test1"
			err := db.Insert(&policy)
			if err != nil {
				t.Fatal(err.Error())
			}
		}

		//setup permissions for test
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
		ad.GrantedPermissions = strings.Join(perms, ",")

		//setup Authorization header for test
		req := assert.HTTPRequest{
			Method:       "GET",
			ExpectStatus: http.StatusOK,
		}
		if !c.AnonymousLogin {
			req.Header = map[string]string{
				"Authorization": keppel.BuildBasicAuthHeader("correctusername", "correctpassword"),
			}
		}

		//build URL query string for test
		query := url.Values{}
		if service != "" {
			query.Set("service", service)
		}
		if c.Scope != "" {
			query.Set("scope", c.Scope)
		}
		req.Path = "/keppel/v1/auth?" + query.Encode()

		//build expected tokenContents to match against
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

		//execute request
		req.Check(t, r)
	}
}

func TestInvalidCredentials(t *testing.T) {
	r, cfg, _, _, _ := setupPrimary(t)
	service := cfg.APIPublicURL.Hostname()

	//execute normal GET requests that would result in a token with granted
	//actions, if we didn't give the wrong username (in the first call) or
	//password (in the second call)
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
	req.Check(t, r)
	req.Header["Authorization"] = "Basic 65082567y2958)*&@@"
	req.Check(t, r)
	req.Header["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte("onlyusername"))
	req.Check(t, r)

	t.Logf("----- test wrong username with service %q -----\n", service)
	req.Header["Authorization"] = keppel.BuildBasicAuthHeader("wrongusername", "correctpassword")
	req.ExpectBody = assert.JSONObject{"details": "wrong credentials"}
	req.Check(t, r)

	t.Logf("----- test wrong password with service %q -----\n", service)
	req.Header["Authorization"] = keppel.BuildBasicAuthHeader("correctusername", "wrongpassword")
	req.Check(t, r)
}

type anycastTestCase struct {
	//request
	AccountName string
	Service     string
	Handler     http.Handler
	//result
	ErrorMessage string
	HasAccess    bool
	Issuer       string
}

func TestAnycastToken(t *testing.T) {
	h1, cfg1, ad1, _, _ := setupPrimary(t)
	h2, cfg2, ad2, _, _ := setupSecondary(t)

	//when reverse-proxying auth requests, the primary registry wants to talk to
	//the secondary via HTTPs, so attach their HTTP handlers to the
	//http.DefaultClient
	tt := &test.RoundTripper{
		Handlers: map[string]http.Handler{
			cfg1.APIPublicURL.Hostname(): h1,
			cfg2.APIPublicURL.Hostname(): h2,
		},
	}
	http.DefaultClient.Transport = tt
	defer func() {
		http.DefaultClient.Transport = nil
	}()

	//setup permissions for test
	perms := fmt.Sprintf("%s:test1authtenant,%s:test1authtenant", keppel.CanPullFromAccount, keppel.CanViewAccount)
	ad1.GrantedPermissions = perms
	ad2.GrantedPermissions = perms

	localService1 := cfg1.APIPublicURL.Hostname()
	localService2 := cfg2.APIPublicURL.Hostname()
	anycastService := cfg1.AnycastAPIPublicURL.Hostname()
	anycastTestCases := []anycastTestCase{
		//when asking for a local token (i.e. not giving the anycast hostname as
		//service), no reverse-proxying is done and we only see the local accounts
		{AccountName: "test1", Service: localService1, Handler: h1,
			HasAccess: true, Issuer: localService1},
		{AccountName: "test2", Service: localService1, Handler: h1,
			HasAccess: false, Issuer: localService1},
		{AccountName: "test1", Service: localService2, Handler: h2,
			HasAccess: false, Issuer: localService2},
		{AccountName: "test2", Service: localService2, Handler: h2,
			HasAccess: true, Issuer: localService2},
		//asking for a token for someone else's local service will never work
		{AccountName: "test1", Service: localService2, Handler: h1,
			ErrorMessage: fmt.Sprintf("cannot issue tokens for service: %q", localService2)},
		{AccountName: "test2", Service: localService2, Handler: h1,
			ErrorMessage: fmt.Sprintf("cannot issue tokens for service: %q", localService2)},
		{AccountName: "test1", Service: localService1, Handler: h2,
			ErrorMessage: fmt.Sprintf("cannot issue tokens for service: %q", localService1)},
		{AccountName: "test2", Service: localService1, Handler: h2,
			ErrorMessage: fmt.Sprintf("cannot issue tokens for service: %q", localService1)},
		//when asking for an anycast token, the request if reverse-proxied if
		//necessary and we will see the Keppel hosting the primary account as
		//issuer
		{AccountName: "test1", Service: anycastService, Handler: h1,
			HasAccess: true, Issuer: localService1},
		{AccountName: "test2", Service: anycastService, Handler: h1,
			HasAccess: true, Issuer: localService2},
		{AccountName: "test1", Service: anycastService, Handler: h2,
			HasAccess: true, Issuer: localService1},
		{AccountName: "test2", Service: anycastService, Handler: h2,
			HasAccess: true, Issuer: localService2},
		//asking for a token for an account that doesn't exist will never work
		{AccountName: "test3", Service: localService1, Handler: h1,
			HasAccess: false, Issuer: localService1},
		{AccountName: "test3", Service: localService2, Handler: h2,
			HasAccess: false, Issuer: localService2},
		{AccountName: "test3", Service: anycastService, Handler: h1,
			HasAccess: false, Issuer: localService1},
		{AccountName: "test3", Service: anycastService, Handler: h2,
			HasAccess: false, Issuer: localService2},
	}

	for idx, c := range anycastTestCases {
		t.Logf("----- testcase %d/%d -----\n", idx+1, len(anycastTestCases))

		req := assert.HTTPRequest{
			Method: "GET",
			Path:   fmt.Sprintf("/keppel/v1/auth?scope=repository:%s/foo:pull&service=%s", c.AccountName, c.Service),
			Header: map[string]string{
				"Authorization": keppel.BuildBasicAuthHeader("correctusername", "correctpassword"),
			},
		}

		if c.ErrorMessage == "" {
			req.ExpectStatus = http.StatusOK

			//build jwtContents struct to contain issued token against
			expectedContents := jwtContents{
				Audience: c.Service,
				Issuer:   "keppel-api@" + c.Issuer,
				Subject:  "correctusername",
			}
			if c.HasAccess {
				expectedContents.Access = []jwtAccess{{
					Type:    "repository",
					Name:    fmt.Sprintf("%s/foo", c.AccountName),
					Actions: []string{"pull"},
				}}
			}
			req.ExpectBody = expectedContents

		} else {
			req.ExpectStatus = http.StatusBadRequest
			req.ExpectBody = assert.JSONObject{"details": c.ErrorMessage}
		}

		req.Check(t, c.Handler)
	}
}
