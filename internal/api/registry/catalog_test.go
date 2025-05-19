// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/sapcc/go-bits/assert"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestCatalogEndpoint(t *testing.T) {
	s := test.NewSetup(t, test.WithAnycast(true))

	// set up dummy accounts for testing
	for idx := 1; idx <= 3; idx++ {
		accountName := models.AccountName(fmt.Sprintf("test%d", idx))
		test.MustExec(t, s.DB, `INSERT INTO accounts (name, auth_tenant_id) VALUES ($1, $2)`, accountName, authTenantID)

		for _, repoName := range []string{"foo", "bar", "qux"} {
			test.MustInsert(t, s.DB, &models.Repository{
				Name:        repoName,
				AccountName: accountName,
			})
		}
	}

	// testcases
	testEmptyCatalog(t, s)
	testNonEmptyCatalog(t, s)
	testDomainRemappedCatalog(t, s)
	testAuthErrorsForCatalog(t, s)
	testNoCatalogOnAnycast(t, s)
}

func testEmptyCatalog(t *testing.T, s test.Setup) {
	// token without any account-level permissions is able to call the endpoint,
	// but cannot list repos in any account, so the list is empty
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

	// query parameters do not influence this result
	req.Path = "/v2/_catalog?n=10"
	req.Check(t, h)
	req.Path = "/v2/_catalog?n=10&last=test1/foo"
	req.Check(t, h)
}

func testNonEmptyCatalog(t *testing.T, s test.Setup) {
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

	// test unpaginated
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/_catalog",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   assert.JSONObject{"repositories": allRepos},
	}.Check(t, h)

	// test paginated
	for offset := range allRepos {
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
					strings.ReplaceAll(lastRepoName, "/", "%2F"), length,
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

	// test error cases for pagination query params
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

func testDomainRemappedCatalog(t *testing.T, s test.Setup) {
	h := s.Handler
	token := s.GetDomainRemappedToken(t, "test1",
		"registry:catalog:*",
		"keppel_account:test1:view",
	)

	// test unpaginated
	assert.HTTPRequest{
		Method: "GET",
		Path:   "/v2/_catalog",
		Header: map[string]string{
			"Authorization":     "Bearer " + token,
			"X-Forwarded-Host":  "test1.registry.example.org",
			"X-Forwarded-Proto": "https",
		},
		ExpectStatus: http.StatusOK,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   assert.JSONObject{"repositories": []string{"bar", "foo", "qux"}},
	}.Check(t, h)

	// test paginated (only a very basic test: we already have most of the test
	// coverage in testNonEmptyCatalog, this mostly checks that the "last"
	// parameter is correctly interpreted as a bare repo name)
	assert.HTTPRequest{
		Method: "GET",
		Path:   "/v2/_catalog?last=foo",
		Header: map[string]string{
			"Authorization":     "Bearer " + token,
			"X-Forwarded-Host":  "test1.registry.example.org",
			"X-Forwarded-Proto": "https",
		},
		ExpectStatus: http.StatusOK,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   assert.JSONObject{"repositories": []string{"qux"}},
	}.Check(t, h)
}

func testAuthErrorsForCatalog(t *testing.T, s test.Setup) {
	// without token, expect auth challenge
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

	// with token for wrong scope, expect Forbidden and renewed auth challenge
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
		// but DENIED is more logical.
		ExpectBody: test.ErrorCode(keppel.ErrDenied),
	}.Check(t, h)

	// without token, expect auth challenge (test for domain-remapped API)
	assert.HTTPRequest{
		Method: "GET",
		Path:   "/v2/_catalog",
		Header: map[string]string{
			"X-Forwarded-Host":  "test1.registry.example.org",
			"X-Forwarded-Proto": "https",
		},
		ExpectStatus: http.StatusUnauthorized,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Www-Authenticate":    `Bearer realm="https://test1.registry.example.org/keppel/v1/auth",service="test1.registry.example.org",scope="registry:catalog:*"`,
			"Content-Type":        "application/json",
		},
		ExpectBody: test.ErrorCode(keppel.ErrUnauthorized),
	}.Check(t, h)
}

func testNoCatalogOnAnycast(t *testing.T, s test.Setup) {
	token := s.GetAnycastToken(t, "registry:catalog:*")
	assert.HTTPRequest{
		Method: "GET",
		Path:   "/v2/_catalog",
		Header: map[string]string{
			"Authorization":     "Bearer " + token,
			"X-Forwarded-Host":  s.Config.AnycastAPIPublicHostname,
			"X-Forwarded-Proto": "https",
		},
		ExpectStatus: http.StatusMethodNotAllowed,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrUnsupported),
	}.Check(t, s.Handler)
}
