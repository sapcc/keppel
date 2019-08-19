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
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

type TestCase struct {
	//request
	ResType string
	ResName string
	Actions string
	//situation
	CannotPush    bool
	CannotPull    bool
	WrongUserName bool
	WrongPassword bool
	WrongService  bool
	//result
	GrantedActions string
}

var testCases = []TestCase{
	//basic success case
	{ResType: "repository", ResName: "test1/foo", Actions: "pull",
		GrantedActions: "pull"},
	{ResType: "repository", ResName: "test1/foo", Actions: "push",
		GrantedActions: "push"},
	{ResType: "repository", ResName: "test1/foo", Actions: "pull,push",
		GrantedActions: "pull,push"},
	//not allowed to pull
	{ResType: "repository", ResName: "test1/foo", Actions: "pull",
		CannotPull: true, GrantedActions: ""},
	{ResType: "repository", ResName: "test1/foo", Actions: "push",
		CannotPull: true, GrantedActions: "push"},
	{ResType: "repository", ResName: "test1/foo", Actions: "pull,push",
		CannotPull: true, GrantedActions: "push"},
	//not allowed to push
	{ResType: "repository", ResName: "test1/foo", Actions: "pull",
		CannotPush: true, GrantedActions: "pull"},
	{ResType: "repository", ResName: "test1/foo", Actions: "push",
		CannotPush: true, GrantedActions: ""},
	{ResType: "repository", ResName: "test1/foo", Actions: "pull,push",
		CannotPush: true, GrantedActions: "pull"},
	//not allowed to pull nor push
	{ResType: "repository", ResName: "test1/foo", Actions: "pull",
		CannotPull: true, CannotPush: true, GrantedActions: ""},
	{ResType: "repository", ResName: "test1/foo", Actions: "push",
		CannotPull: true, CannotPush: true, GrantedActions: ""},
	{ResType: "repository", ResName: "test1/foo", Actions: "pull,push",
		CannotPull: true, CannotPush: true, GrantedActions: ""},
}

//TODO all useful combinations of testcases
//  - WrongUserName
//  - WrongPassword
//  - WrongService
//  - scope "registry:catalog:*"
//  - check line coverage for internal/api/auth/token.go
//TODO token is always 200, even without authz
//TODO expect refresh_token when offline_token=true is given

func TestAuth(t *testing.T) {
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

	for idx, c := range testCases {
		t.Logf("----- testcase %d/%d -----\n", idx+1, len(testCases))
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
		authInput := "correctusername:"
		if c.WrongUserName {
			authInput = "wrongusername:"
		}
		if c.WrongPassword {
			authInput += "wrongpassword"
		} else {
			authInput += "correctpassword"
		}
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(authInput)))

		//build URL query string for test
		query := url.Values{
			"service": {"registry.example.org"},
		}
		if c.WrongService {
			query.Set("service", "wrongregistry.example.org")
		}
		switch {
		case c.ResType == "" && c.ResName == "" && c.Actions == "":
			query.Set("offline_token", "true")
		case c.ResType != "" && c.ResName != "" && c.Actions != "":
			query.Set("scope", c.ResType+":"+c.ResName+":"+c.Actions)
		default:
			t.Error("unexpected combination of restype/resname/actions")
		}
		req.URL.RawQuery = query.Encode()

		//execute request
		recorder := httptest.NewRecorder()
		r.ServeHTTP(recorder, req)
		resp := recorder.Result()

		//always expect token in result
		var responseBody struct {
			Token string `json:"token"`
			//optional fields (all listed so that we can use DisallowUnknownFields())
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    uint64 `json:"expires_in"`
			IssuedAt     string `json:"issued_at"`
		}
		dec := json.NewDecoder(resp.Body)
		dec.DisallowUnknownFields()
		err := dec.Decode(&responseBody)
		if err != nil {
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
			expectedAccess = []jwtAccess{{
				Type:    c.ResType,
				Name:    c.ResName,
				Actions: strings.Split(c.GrantedActions, ","),
			}}
		}
		equal := assert.DeepEqual(t, "token.Access", token.Access, expectedAccess)
		if !equal {
			continue
		}
		//TODO many attributes missing

	}
}
