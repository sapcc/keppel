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

package registryv2

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

var authorizationHeader = "Basic " + base64.StdEncoding.EncodeToString(
	[]byte("correctusername:correctpassword"),
)

func getToken(t *testing.T, h http.Handler, adGeneric keppel.AuthDriver, scope string, perms ...keppel.Permission) string {
	t.Helper()

	//configure AuthDriver to allow access for this call
	ad := adGeneric.(*test.AuthDriver)
	ad.ExpectedUserName = "correctusername"
	ad.ExpectedPassword = "correctpassword"
	permStrs := make([]string, len(perms))
	for idx, perm := range perms {
		permStrs[idx] = string(perm) + ":test1authtenant"
	}
	ad.GrantedPermissions = strings.Join(permStrs, ",")

	//build a token request
	query := url.Values{}
	query.Set("service", "registry.example.org")
	if scope != "" {
		query.Set("scope", scope)
	}
	_, bodyBytes := assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/auth?" + query.Encode(),
		Header:       map[string]string{"Authorization": authorizationHeader},
		ExpectStatus: http.StatusOK,
	}.Check(t, h)

	var data struct {
		Token string `json:"token"`
	}
	err := json.Unmarshal(bodyBytes, &data)
	if err != nil {
		t.Fatal(err.Error())
	}
	return data.Token
}

////////////////////////////////////////////////////////////////////////////////
//errorCode wraps keppel.RegistryV2ErrorCode with an implementation of the
//assert.HTTPResponseBody interface.

type errorCode keppel.RegistryV2ErrorCode

func (e errorCode) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) {
	t.Helper()
	var data struct {
		Errors []struct {
			Code errorCode `json:"code"`
		} `json:"errors"`
	}
	err := json.Unmarshal(responseBody, &data)
	if err != nil {
		t.Errorf("%s: cannot decode JSON: %s", requestInfo, err.Error())
		t.Logf("\tresponse body = %q", string(responseBody))
		return
	}

	if len(data.Errors) != 1 || data.Errors[0].Code != e {
		t.Errorf(requestInfo + ": got unexpected error")
		t.Logf("\texpected = %q\n", string(e))
		t.Logf("\tactual = %q\n", string(responseBody))
	}
}
