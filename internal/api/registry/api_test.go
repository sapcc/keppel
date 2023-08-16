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

package registryv2_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/sapcc/go-bits/assert"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestVersionCheckEndpoint(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler

		//without token, expect auth challenge
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/v2/",
			Header:       test.AddHeadersForCorrectAuthChallenge(nil),
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
		token := s.GetToken(t /*, no scopes */)
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/v2/",
			Header:       map[string]string{"Authorization": "Bearer " + token},
			ExpectStatus: http.StatusOK,
			ExpectHeader: test.VersionHeader,
		}.Check(t, h)
	})
}

func TestKeppelAPIAuth(t *testing.T) {
	//All the other tests use the conventional auth method using bearer tokens.
	//This test provides test coverage for authenticating with the same
	//AuthDriver-dependent mechanism used by the Keppel API.
	testWithPrimary(t, nil, func(s test.Setup) {
		//upload a manifest for testing (using bearer tokens since all our test
		//helper functions use those)
		h := s.Handler
		image := test.GenerateImage(test.GenerateExampleLayer(1))
		s.Clock.StepBy(time.Second)
		image.MustUpload(t, s, fooRepoRef, "first")

		//test scopeless endpoint: happy case
		assert.HTTPRequest{
			Method: "GET",
			Path:   "/v2/",
			Header: map[string]string{
				"Authorization": "keppel",
				"X-Test-Perms":  "view:test1authtenant",
			},
			ExpectStatus: http.StatusOK,
			ExpectHeader: test.VersionHeader,
		}.Check(t, h)
		//test scopeless endpoint: failure case ("Authorization: keppel" means that
		//we want Keppel API auth, but then we don't pass the respective headers,
		//so we get a 401; we do not get an auth challenge since Keppel API auth
		//does not work with auth challenges)
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/v2/",
			Header:       test.AddHeadersForCorrectAuthChallenge(map[string]string{"Authorization": "keppel"}),
			ExpectStatus: http.StatusUnauthorized,
			ExpectHeader: map[string]string{
				test.VersionHeaderKey: test.VersionHeaderValue,
				"Www-Authenticate":    "",
			},
		}.Check(t, h)

		//test catalog endpoint: happy case
		assert.HTTPRequest{
			Method: "GET",
			Path:   "/v2/_catalog",
			Header: map[string]string{
				"Authorization": "keppel",
				"X-Test-Perms":  "view:test1authtenant",
			},
			ExpectStatus: http.StatusOK,
			ExpectHeader: test.VersionHeader,
			ExpectBody: assert.JSONObject{
				"repositories": []string{"test1/foo"},
			},
		}.Check(t, h)
		//test catalog endpoint: "failure" case (no access to account -> empty list)
		assert.HTTPRequest{
			Method: "GET",
			Path:   "/v2/_catalog",
			Header: map[string]string{
				"Authorization": "keppel",
				"X-Test-Perms":  "view:someothertenant",
			},
			ExpectStatus: http.StatusOK,
			ExpectHeader: test.VersionHeader,
			ExpectBody: assert.JSONObject{
				"repositories": []string{},
			},
		}.Check(t, h)

		//test repository-scoped endpoint: happy case
		assert.HTTPRequest{
			Method: "GET",
			Path:   "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
			Header: map[string]string{
				"Authorization": "keppel",
				"X-Test-Perms":  "view:test1authtenant,pull:test1authtenant",
			},
			ExpectStatus: http.StatusOK,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   assert.ByteData(image.Manifest.Contents),
		}.Check(t, h)
		//test repository-scoped endpoint: failure case (no pull permission)
		assert.HTTPRequest{
			Method: "GET",
			Path:   "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
			Header: test.AddHeadersForCorrectAuthChallenge(map[string]string{
				"Authorization": "keppel",
				"X-Test-Perms":  "view:test1authtenant",
			}),
			ExpectStatus: http.StatusUnauthorized,
			ExpectHeader: map[string]string{
				test.VersionHeaderKey: test.VersionHeaderValue,
				"Www-Authenticate":    "", //Keppel API auth does not use auth challenges
			},
		}.Check(t, h)
	})
}
