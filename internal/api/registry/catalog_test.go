// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/majewsky/gg/jsonmatch"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/httptest"
	"github.com/sapcc/go-bits/must"

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
			must.SucceedT(t, s.DB.Insert(&models.Repository{
				Name:        repoName,
				AccountName: accountName,
			}))
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
	ctx := t.Context()
	h := s.Handler
	token := s.GetToken(t, "registry:catalog:*")
	expectedBody := jsonmatch.Object{"repositories": []string{}}
	for _, path := range []string{"/v2/_catalog", "/v2/_catalog?n=10", "/v2/_catalog?n=10&last=test1/foo"} {
		resp := h.RespondTo(ctx, "GET "+path, httptest.WithHeader("Authorization", "Bearer "+token))
		resp.ExpectJSON(t, http.StatusOK, expectedBody)
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
	}
}

func testNonEmptyCatalog(t *testing.T, s test.Setup) {
	ctx := t.Context()
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
	resp := h.RespondTo(ctx, "GET /v2/_catalog", httptest.WithHeader("Authorization", "Bearer "+token))
	resp.ExpectJSON(t, http.StatusOK, jsonmatch.Object{"repositories": allRepos})
	assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)

	// test paginated
	for offset := range allRepos {
		for length := 1; length <= len(allRepos)+1; length++ {
			expectedPage := allRepos[offset:]

			if len(expectedPage) > length {
				expectedPage = expectedPage[:length]
				lastRepoName := expectedPage[len(expectedPage)-1]
				expectedLink := fmt.Sprintf(`</v2/_catalog?last=%s&n=%d>; rel="next"`,
					strings.ReplaceAll(lastRepoName, "/", "%2F"), length,
				)

				path := fmt.Sprintf(`/v2/_catalog?n=%d`, length)
				if offset > 0 {
					path += `&last=` + allRepos[offset-1] //nolint:gosec // slice index is clearly not out of range
				}

				resp = h.RespondTo(ctx, "GET "+path, httptest.WithHeader("Authorization", "Bearer "+token))
				resp.ExpectJSON(t, http.StatusOK, jsonmatch.Object{"repositories": expectedPage})
				assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
				assert.Equal(t, resp.Header().Get("Content-Type"), "application/json")
				assert.Equal(t, resp.Header().Get("Link"), expectedLink)
			} else {
				path := fmt.Sprintf(`/v2/_catalog?n=%d`, length)
				if offset > 0 {
					path += `&last=` + allRepos[offset-1] //nolint:gosec // slice index is clearly not out of range
				}

				resp = h.RespondTo(ctx, "GET "+path, httptest.WithHeader("Authorization", "Bearer "+token))
				resp.ExpectJSON(t, http.StatusOK, jsonmatch.Object{"repositories": expectedPage})
				assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
				assert.Equal(t, resp.Header().Get("Content-Type"), "application/json")
			}
		}
	}

	// test error cases for pagination query params
	resp = h.RespondTo(ctx, "GET /v2/_catalog?n=-1", httptest.WithHeader("Authorization", "Bearer "+token))
	resp.ExpectText(t, http.StatusBadRequest, "invalid value for \"n\": strconv.ParseUint: parsing \"-1\": invalid syntax\n")
	assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)

	resp = h.RespondTo(ctx, "GET /v2/_catalog?n=0", httptest.WithHeader("Authorization", "Bearer "+token))
	resp.ExpectText(t, http.StatusBadRequest, "invalid value for \"n\": must not be 0\n")
	assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)

	resp = h.RespondTo(ctx, "GET /v2/_catalog?n=10&last=invalid", httptest.WithHeader("Authorization", "Bearer "+token))
	resp.ExpectText(t, http.StatusBadRequest, "invalid value for \"last\": must contain a slash\n")
	assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
}

func testDomainRemappedCatalog(t *testing.T, s test.Setup) {
	ctx := t.Context()
	h := s.Handler
	token := s.GetDomainRemappedToken(t, "test1",
		"registry:catalog:*",
		"keppel_account:test1:view",
	)

	// test unpaginated
	resp := h.RespondTo(ctx, "GET /v2/_catalog",
		httptest.WithHeader("Authorization", "Bearer "+token),
		httptest.WithHeader("X-Forwarded-Host", "test1.registry.example.org"),
		httptest.WithHeader("X-Forwarded-Proto", "https"))
	resp.ExpectJSON(t, http.StatusOK, jsonmatch.Object{"repositories": []string{"bar", "foo", "qux"}})
	assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)

	// test paginated (only a very basic test: we already have most of the test
	// coverage in testNonEmptyCatalog, this mostly checks that the "last"
	// parameter is correctly interpreted as a bare repo name)
	resp = h.RespondTo(ctx, "GET /v2/_catalog?last=foo",
		httptest.WithHeader("Authorization", "Bearer "+token),
		httptest.WithHeader("X-Forwarded-Host", "test1.registry.example.org"),
		httptest.WithHeader("X-Forwarded-Proto", "https"))
	resp.ExpectJSON(t, http.StatusOK, jsonmatch.Object{"repositories": []string{"qux"}})
	assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
}

func testAuthErrorsForCatalog(t *testing.T, s test.Setup) {
	// without token, expect auth challenge
	ctx := t.Context()
	h := s.Handler
	resp := h.RespondTo(ctx, "GET /v2/_catalog")
	resp.ExpectJSON(t, http.StatusUnauthorized, errCodeJSON(keppel.ErrUnauthorized))
	assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
	assert.Equal(t, resp.Header().Get("Www-Authenticate"), `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="registry:catalog:*"`)
	assert.Equal(t, resp.Header().Get("Content-Type"), "application/json")

	// with token for wrong scope, expect Forbidden and renewed auth challenge
	token := s.GetToken(t, "repository:test1/foo:pull")
	resp = h.RespondTo(ctx, "GET /v2/_catalog", httptest.WithHeader("Authorization", "Bearer "+token))
	//NOTE: Docker Hub (https://registry-1.docker.io) sends UNAUTHORIZED here,
	// but DENIED is more logical.
	resp.ExpectJSON(t, http.StatusUnauthorized, errCodeJSON(keppel.ErrDenied))
	assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
	assert.Equal(t, resp.Header().Get("Www-Authenticate"), `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="registry:catalog:*",error="insufficient_scope"`)
	assert.Equal(t, resp.Header().Get("Content-Type"), "application/json")

	// without token, expect auth challenge (test for domain-remapped API)
	resp = h.RespondTo(ctx, "GET /v2/_catalog",
		httptest.WithHeader("X-Forwarded-Host", "test1.registry.example.org"),
		httptest.WithHeader("X-Forwarded-Proto", "https"))
	resp.ExpectJSON(t, http.StatusUnauthorized, errCodeJSON(keppel.ErrUnauthorized))
	assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
	assert.Equal(t, resp.Header().Get("Www-Authenticate"), `Bearer realm="https://test1.registry.example.org/keppel/v1/auth",service="test1.registry.example.org",scope="registry:catalog:*"`)
	assert.Equal(t, resp.Header().Get("Content-Type"), "application/json")
}

func testNoCatalogOnAnycast(t *testing.T, s test.Setup) {
	ctx := t.Context()
	token := s.GetAnycastToken(t, "registry:catalog:*")
	resp := s.Handler.RespondTo(ctx, "GET /v2/_catalog",
		httptest.WithHeader("Authorization", "Bearer "+token),
		httptest.WithHeader("X-Forwarded-Host", s.Config.AnycastAPIPublicHostname),
		httptest.WithHeader("X-Forwarded-Proto", "https"))
	resp.ExpectJSON(t, http.StatusMethodNotAllowed, errCodeJSON(keppel.ErrUnsupported))
	assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
}
