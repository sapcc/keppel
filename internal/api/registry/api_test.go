/******************************************************************************
*
*  Copyright 2020 SAP SE
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
	"net/http"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestVersionCheckEndpoint(t *testing.T) {
	h, _, _, ad, _, _ := setup(t, nil)

	//without token, expect auth challenge
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/",
		Header:       addHeadersForCorrectAuthChallenge(nil),
		ExpectStatus: http.StatusUnauthorized,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Www-Authenticate":    `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org"`,
		},
		ExpectBody: assert.JSONObject{
			"errors": []assert.JSONObject{{
				"code":    keppel.ErrUnauthorized,
				"detail":  nil,
				"message": "no bearer token found in request headers",
			}},
		},
	}.Check(t, h)

	//with token, expect status code 200
	token := getToken(t, h, ad, "" /* , no permissions */)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: test.VersionHeader,
	}.Check(t, h)
}
