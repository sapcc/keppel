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

package registryv2_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestCatalogEndpoint(t *testing.T) {
	s := test.NewSetup(t)

	//set up dummy accounts for testing
	for idx := 1; idx <= 3; idx++ {
		err := s.DB.Insert(&keppel.Account{
			Name:           fmt.Sprintf("test%d", idx),
			AuthTenantID:   authTenantID,
			GCPoliciesJSON: "[]",
		})
		if err != nil {
			t.Fatal(err.Error())
		}

		for _, repoName := range []string{"foo", "bar", "qux"} {
			err := s.DB.Insert(&keppel.Repository{
				Name:        repoName,
				AccountName: fmt.Sprintf("test%d", idx),
			})
			if err != nil {
				t.Fatal(err.Error())
			}
		}
	}

	//testcases
	testEmptyCatalog(t, s)
	testNonEmptyCatalog(t, s)
	testAuthErrorsForCatalog(t, s)
}

func testEmptyCatalog(t *testing.T, s test.Setup) {
	//token without any account-level permissions is able to call the endpoint,
	//but cannot list repos in any account, so the list is empty
	h := s.Handler
	token := s.GetToken(t, "registry:catalog:*")

	req := assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/_catalog",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: test.VersionHeader,
		ExpectBody: assert.JSONObject{
			"repositories": []string{},
		},
	}
	req.Check(t, h)

	//query parameters do not influence this result
	req.Path = "/v2/_catalog?n=10"
	req.Check(t, h)
	req.Path = "/v2/_catalog?n=10&last=test1/foo"
	req.Check(t, h)
}

func testNonEmptyCatalog(t *testing.T, s test.Setup) {
	//token with keppel.CanViewAccount can read all accounts' catalogs
	h := s.Handler
	token := s.GetToken(t,
		"registry:catalog:*",
		"keppel_account:test1:view",
		"keppel_account:test2:view",
		"keppel_account:test3:view",
	)

	allRepos := []string{
		"test1/bar",
		"test1/foo",
		"test1/qux",
		"test2/bar",
		"test2/foo",
		"test2/qux",
		"test3/bar",
		"test3/foo",
		"test3/qux",
	}

	//test unpaginated
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/_catalog",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   assert.JSONObject{"repositories": allRepos},
	}.Check(t, h)

	//test paginated
	for offset := 0; offset < len(allRepos); offset++ {
		for length := 1; length <= len(allRepos)+1; length++ {
			expectedPage := allRepos[offset:]
			expectedHeaders := map[string]string{
				test.VersionHeaderKey: test.VersionHeaderValue,
				"Content-Type":        "application/json",
			}

			if len(expectedPage) > length {
				expectedPage = expectedPage[:length]
				lastRepoName := expectedPage[len(expectedPage)-1]
				expectedHeaders["Link"] = fmt.Sprintf(`</v2/_catalog?last=%s&n=%d>; rel="next"`,
					strings.Replace(lastRepoName, "/", "%2F", -1), length,
				)
			}

			path := fmt.Sprintf(`/v2/_catalog?n=%d`, length)
			if offset > 0 {
				path += `&last=` + allRepos[offset-1]
			}

			assert.HTTPRequest{
				Method:       "GET",
				Path:         path,
				Header:       map[string]string{"Authorization": "Bearer " + token},
				ExpectStatus: http.StatusOK,
				ExpectHeader: expectedHeaders,
				ExpectBody:   assert.JSONObject{"repositories": expectedPage},
			}.Check(t, h)
		}
	}

	//test error cases for pagination query params
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/_catalog?n=-1",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusBadRequest,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   assert.StringData("invalid value for \"n\": strconv.ParseUint: parsing \"-1\": invalid syntax\n"),
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/_catalog?n=0",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusBadRequest,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   assert.StringData("invalid value for \"n\": must not be 0\n"),
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/_catalog?n=10&last=invalid",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusBadRequest,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   assert.StringData("invalid value for \"last\": must contain a slash\n"),
	}.Check(t, h)
}

func testAuthErrorsForCatalog(t *testing.T, s test.Setup) {
	//without token, expect auth challenge
	h := s.Handler
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/_catalog",
		Header:       test.AddHeadersForCorrectAuthChallenge(nil),
		ExpectStatus: http.StatusUnauthorized,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Www-Authenticate":    `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="registry:catalog:*"`,
			"Content-Type":        "application/json",
		},
		ExpectBody: test.ErrorCode(keppel.ErrUnauthorized),
	}.Check(t, h)

	//with token for wrong scope, expect Forbidden and renewed auth challenge
	token := s.GetToken(t, "repository:test1/foo:pull")
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/_catalog",
		Header:       test.AddHeadersForCorrectAuthChallenge(map[string]string{"Authorization": "Bearer " + token}),
		ExpectStatus: http.StatusUnauthorized,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Www-Authenticate":    `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="registry:catalog:*",error="insufficient_scope"`,
			"Content-Type":        "application/json",
		},
		//NOTE: Docker Hub (https://registry-1.docker.io) sends UNAUTHORIZED here,
		//but DENIED is more logical.
		ExpectBody: test.ErrorCode(keppel.ErrDenied),
	}.Check(t, h)
}
