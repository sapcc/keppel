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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/assert"
	authapi "github.com/sapcc/keppel/internal/api/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestCatalogEndpoint(t *testing.T) {
	cfg, db := test.Setup(t)

	//set up dummy accounts for testing
	for idx := 1; idx <= 3; idx++ {
		err := db.Insert(&keppel.Account{
			Name:         fmt.Sprintf("test%d", idx),
			AuthTenantID: "test1authtenant",
		})
		if err != nil {
			t.Fatal(err.Error())
		}
	}

	//setup auth API with the regular unittest driver
	ad, err := keppel.NewAuthDriver("unittest")
	if err != nil {
		t.Fatal(err.Error())
	}
	r := mux.NewRouter()
	authapi.NewAPI(cfg, ad, db).AddTo(r)

	//setup registry API with a specialized mock orchestration driver
	od := keppel.OrchestrationDriver(dummyCatalogDriver{})
	NewAPI(cfg, od, db).AddTo(r)

	//testcases
	testEmptyCatalog(t, r, ad)
	testNonEmptyCatalog(t, r, ad)
	testAuthErrorsForCatalog(t, r, ad)
}

func testEmptyCatalog(t *testing.T, h http.Handler, ad keppel.AuthDriver) {
	//token without any permissions is able to call the endpoint, but cannot list
	//repos in any account, so the list is empty
	token := getToken(t, h, ad, "registry:catalog:*" /* , no permissions */)

	req := assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/_catalog",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: versionHeader,
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

func testNonEmptyCatalog(t *testing.T, h http.Handler, ad keppel.AuthDriver) {
	//token with keppel.CanViewAccount can read all accounts' catalogs
	token := getToken(t, h, ad, "registry:catalog:*", keppel.CanViewAccount)

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
		ExpectHeader: versionHeader,
		ExpectBody:   assert.JSONObject{"repositories": allRepos},
	}.Check(t, h)

	//test paginated
	for offset := 0; offset < len(allRepos); offset++ {
		for length := 1; length <= len(allRepos)+1; length++ {
			expectedPage := allRepos[offset:]
			expectedHeaders := map[string]string{
				versionHeaderKey: versionHeaderValue,
				"Content-Type":   "application/json",
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
		ExpectHeader: versionHeader,
		ExpectBody:   assert.StringData("invalid value for \"n\": strconv.ParseUint: parsing \"-1\": invalid syntax\n"),
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/_catalog?n=0",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusBadRequest,
		ExpectHeader: versionHeader,
		ExpectBody:   assert.StringData("invalid value for \"n\": must not be 0\n"),
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/_catalog?n=10&last=invalid",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusBadRequest,
		ExpectHeader: versionHeader,
		ExpectBody:   assert.StringData("invalid value for \"last\": must contain a slash\n"),
	}.Check(t, h)
}

func testAuthErrorsForCatalog(t *testing.T, h http.Handler, ad keppel.AuthDriver) {
	//without token, expect auth challenge
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/_catalog",
		ExpectStatus: http.StatusUnauthorized,
		ExpectHeader: map[string]string{
			versionHeaderKey:   versionHeaderValue,
			"Www-Authenticate": `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="registry:catalog:*"`,
			"Content-Type":     "application/json",
		},
		ExpectBody: errorCode(keppel.ErrUnauthorized),
	}.Check(t, h)

	//with token for wrong scope, expect Forbidden and renewed auth challenge
	token := getToken(t, h, ad, "repository:test1/foo:pull", keppel.CanPullFromAccount)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/_catalog",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusForbidden,
		ExpectHeader: map[string]string{
			versionHeaderKey:   versionHeaderValue,
			"Www-Authenticate": `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="registry:catalog:*",error="insufficient_scope"`,
			"Content-Type":     "application/json",
		},
		//NOTE: Docker Hub (https://registry-1.docker.io) sends UNAUTHORIZED here,
		//but DENIED is more logical.
		ExpectBody: errorCode(keppel.ErrDenied),
	}.Check(t, h)
}

////////////////////////////////////////////////////////////////////////////////
// type dummyCatalogDriver

type dummyCatalogDriver struct{}

//DoHTTPRequest implements the keppel.OrchestrationDriver interface.
func (dummyCatalogDriver) DoHTTPRequest(account keppel.Account, r *http.Request, opts keppel.RequestOptions) (*http.Response, error) {
	//expect only catalog queries
	if r.Method != "GET" || r.URL.Path != "/v2/_catalog" {
		return nil, fmt.Errorf("expected request for GET /v2/_catalog, got %s %s", r.Method, r.URL.Path)
	}
	if r.URL.RawQuery != "" {
		return nil, fmt.Errorf("expected no query string for GET /v2/_catalog, got %q", r.URL.RawQuery)
	}

	buf, _ := json.Marshal(map[string]interface{}{
		"repositories": []string{
			//NOTE: not sorted, but catalog endpoint of keppel-api will do proper sorting
			account.Name + "/foo",
			account.Name + "/bar",
			account.Name + "/qux",
		},
	})

	return &http.Response{
		Status:        "200 OK",
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": {"application/json"}},
		Body:          ioutil.NopCloser(bytes.NewReader(buf)),
		ContentLength: int64(len(buf)),
		Request:       r,
	}, nil
}

//Run implements the keppel.OrchestrationDriver interface.
func (dummyCatalogDriver) Run(ctx context.Context) (ok bool) {
	return true
}
