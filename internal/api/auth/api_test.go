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
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/assert"
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
	Scope string
	//situation
	CannotPush bool
	CannotPull bool
	//result
	GrantedActions string
}

var testCases = []TestCase{
	//basic success case
	{Scope: "repository:test1/foo:pull",
		GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		GrantedActions: "push"},
	{Scope: "repository:test1/foo:pull,push",
		GrantedActions: "pull,push"},
	//not allowed to pull
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, GrantedActions: ""},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, GrantedActions: "push"},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, GrantedActions: "push"},
	//not allowed to push
	{Scope: "repository:test1/foo:pull",
		CannotPush: true, GrantedActions: "pull"},
	{Scope: "repository:test1/foo:push",
		CannotPush: true, GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		CannotPush: true, GrantedActions: "pull"},
	//not allowed to pull nor push
	{Scope: "repository:test1/foo:pull",
		CannotPull: true, CannotPush: true, GrantedActions: ""},
	{Scope: "repository:test1/foo:push",
		CannotPull: true, CannotPush: true, GrantedActions: ""},
	{Scope: "repository:test1/foo:pull,push",
		CannotPull: true, CannotPush: true, GrantedActions: ""},
	//catalog access always allowed if username/password are ok (access to
	//specific accounts is filtered later)
	{Scope: "registry:catalog:*",
		GrantedActions: "*"},
	{Scope: "registry:catalog:*",
		CannotPull: true, GrantedActions: "*"},
	{Scope: "registry:catalog:*",
		CannotPush: true, GrantedActions: "*"},
	{Scope: "registry:catalog:*",
		CannotPull: true, CannotPush: true, GrantedActions: "*"},
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
}

//TODO all useful combinations of testcases
//  - WrongUserName
//  - WrongPassword
//  - check line coverage for internal/api/auth/token.go
//TODO token is always 200, even without authz
//TODO expect refresh_token when offline_token=true is given
//TODO find out what's up with CompiledScopes
//TODO pull all code from internal/auth/ into internal/api/auth/ that is not
//     useful outside of this package
//
//NOTES:
//
//$ curl -H "Authorization: soughwotruhwtyhet" -i https://auth.docker.io/token'?service=registry.docker.io&scope=foo:bar:*'
//  HTTP/1.1 400 Bad Request
//  Content-Type: text/plain; charset=utf-8
//
//  {"details":"malformed HTTP Authorization header"}
//
//$ curl -H "Authorization: Basic Zm9vOmJhciAtbgo=" -i https://auth.docker.io/token'?service=registry.docker.io&scope=foo:bar:*'
//  HTTP/1.1 401 Unauthorized
//  Www-Authenticate: Basic realm="auth.docker.io"
//  Content-Type: text/plain; charset=utf-8
//
//  {"details":"incorrect username or password"}

func foreachServiceValue(action func(serviceStr string)) {
	//Every value for the ?service= query parameter is equally okay and the API
	//should issue a token for exactly that audience.
	action("registry.example.com")
	action("K='i}1&} ")
	action("")
}

func TestAuthBasic(t *testing.T) {
	cfg, db := test.Setup(t)

	//set up a dummy account for testing
	err := db.Insert(&keppel.Account{
		Name:         "test1",
		AuthTenantID: "test1authtenant",
	})
	if err != nil {
		t.Fatal(err.Error())
	}

	adGeneric, err := keppel.NewAuthDriver("unittest")
	if err != nil {
		t.Fatal(err.Error())
	}
	ad := adGeneric.(*test.AuthDriver)
	ad.ExpectedUserName = "correctusername"
	ad.ExpectedPassword = "correctpassword"

	r := mux.NewRouter()
	NewAPI(cfg, ad, db).AddTo(r)

	foreachServiceValue(func(service string) {
		for idx, c := range testCases {
			t.Logf("----- testcase %d/%d with service %q -----\n", idx+1, len(testCases), service)
			req := httptest.NewRequest("GET", "/keppel/v1/auth", nil)

			//setup permissions for test
			var perms []string
			if c.CannotPush {
				perms = append(perms, string(keppel.CanPushToAccount)+":otheraccount")
			} else {
				perms = append(perms, string(keppel.CanPushToAccount)+":test1authtenant")
			}
			if c.CannotPull {
				perms = append(perms, string(keppel.CanPullFromAccount)+":otheraccount")
			} else {
				perms = append(perms, string(keppel.CanPullFromAccount)+":test1authtenant")
			}
			ad.GrantedPermissions = strings.Join(perms, ",")

			//setup Authorization header for test
			authInput := "correctusername:correctpassword"
			req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(authInput)))

			//build URL query string for test
			query := url.Values{}
			if service != "" {
				query.Set("service", service)
			}
			if c.Scope != "" {
				query.Set("scope", c.Scope)
			}
			req.URL.RawQuery = query.Encode()

			//execute request
			recorder := httptest.NewRecorder()
			r.ServeHTTP(recorder, req)
			resp := recorder.Result()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected status 200, got %d instead", resp.StatusCode)
			}

			//always expect token in result
			responseBodyBytes, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err.Error())
			}
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
			err = dec.Decode(&responseBody)
			if err != nil {
				t.Logf("token was: %s", string(responseBodyBytes))
				t.Error(err.Error())
				continue
			}

			//extract payload from token
			tokenFields := strings.Split(responseBody.Token, ".")
			if len(tokenFields) != 3 {
				t.Logf("JWT is %s", string(responseBody.Token))
				t.Errorf("expected token with 3 parts, got %d parts", len(tokenFields))
				continue
			}
			tokenBytes, err := base64.RawURLEncoding.DecodeString(tokenFields[1])
			if err != nil {
				t.Error(err.Error())
				continue
			}

			//decode token
			type jwtAccess struct {
				Type    string   `json:"type"`
				Name    string   `json:"name"`
				Actions []string `json:"actions"`
			}
			var token struct {
				Issuer    string      `json:"iss"`
				Subject   string      `json:"sub"`
				Audience  string      `json:"aud"`
				ExpiresAt int64       `json:"exp"`
				NotBefore int64       `json:"nbf"`
				IssuedAt  int64       `json:"iat"`
				TokenID   string      `json:"jti"`
				Access    []jwtAccess `json:"access"`
			}
			dec = json.NewDecoder(bytes.NewReader(tokenBytes))
			dec.DisallowUnknownFields()
			err = dec.Decode(&token)
			if err != nil {
				t.Logf("token JSON is %s", string(tokenBytes))
				t.Error(err.Error())
				continue
			}

			//check token attributes for correctness
			expectedAccess := []jwtAccess(nil)
			if c.GrantedActions != "" {
				fields := strings.SplitN(c.Scope, ":", 3)
				expectedAccess = []jwtAccess{{
					Type:    fields[0],
					Name:    fields[1],
					Actions: strings.Split(c.GrantedActions, ","),
				}}
			}
			equal := true
			if !assert.DeepEqual(t, "token.Access", token.Access, expectedAccess) {
				equal = false
			}
			if !assert.DeepEqual(t, "token.Audience", token.Audience, service) {
				equal = false
			}
			if !equal {
				continue
			}
			//TODO many attributes missing

		}
	})
}
