// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/majewsky/gg/jsonmatch"
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
	tokenHeaders := s.GetTokenHeaders(t, "registry:catalog:*")

	// query parameters do not influence this result
	for _, query := range []string{"", "?n=10", "?n=10&last=test1/foo"} {
		t.Run("GET /v2/_catalog"+query, func(t *testing.T) {
			s.RespondTo(ctx, "GET /v2/_catalog"+query, httptest.WithHeaders(tokenHeaders)).
				ExpectJSON(t, http.StatusOK, jsonmatch.Object{"repositories": []string{}})
		})
	}
}

func testNonEmptyCatalog(t *testing.T, s test.Setup) {
	ctx := t.Context()
	tokenHeaders := s.GetTokenHeaders(t,
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
	s.RespondTo(ctx, "GET /v2/_catalog", httptest.WithHeaders(tokenHeaders)).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{"repositories": allRepos})

	// test paginated
	for offset := range allRepos {
		for length := 1; length <= len(allRepos)+1; length++ {
			expectedPage := allRepos[offset:]
			expectedHeaders := http.Header{
				"Content-Type": {"application/json"},
			}

			if len(expectedPage) > length {
				expectedPage = expectedPage[:length]
				lastRepoName := expectedPage[len(expectedPage)-1]
				expectedHeaders.Set("Link", fmt.Sprintf(`</v2/_catalog?last=%s&n=%d>; rel="next"`,
					strings.ReplaceAll(lastRepoName, "/", "%2F"), length,
				))
			}

			methodAndPath := fmt.Sprintf(`GET /v2/_catalog?n=%d`, length)
			if offset > 0 {
				methodAndPath += `&last=` + allRepos[offset-1] //nolint:gosec // slice index is clearly not out of range
			}

			s.RespondTo(ctx, methodAndPath, httptest.WithHeaders(tokenHeaders)).
				ExpectHeaders(t, expectedHeaders).
				ExpectJSON(t, http.StatusOK, jsonmatch.Object{"repositories": expectedPage})
		}
	}

	// test error cases for pagination query params
	s.RespondTo(ctx, "GET /v2/_catalog?n=-1", httptest.WithHeaders(tokenHeaders)).
		ExpectText(t, http.StatusBadRequest, "invalid value for \"n\": strconv.ParseUint: parsing \"-1\": invalid syntax\n")
	s.RespondTo(ctx, "GET /v2/_catalog?n=0", httptest.WithHeaders(tokenHeaders)).
		ExpectText(t, http.StatusBadRequest, "invalid value for \"n\": must not be 0\n")
	s.RespondTo(ctx, "GET /v2/_catalog?n=10&last=invalid", httptest.WithHeaders(tokenHeaders)).
		ExpectText(t, http.StatusBadRequest, "invalid value for \"last\": must contain a slash\n")
}

func testDomainRemappedCatalog(t *testing.T, s test.Setup) {
	ctx := t.Context()
	tokenHeaders := s.GetDomainRemappedTokenHeaders(t, "test1",
		"registry:catalog:*",
		"keppel_account:test1:view",
	)

	// test unpaginated
	s.RespondTo(ctx, "GET /v2/_catalog", httptest.WithHeaders(tokenHeaders)).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{"repositories": []string{"bar", "foo", "qux"}})

	// test paginated (only a very basic test: we already have most of the test
	// coverage in testNonEmptyCatalog, this mostly checks that the "last"
	// parameter is correctly interpreted as a bare repo name)
	s.RespondTo(ctx, "GET /v2/_catalog?last=foo", httptest.WithHeaders(tokenHeaders)).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{"repositories": []string{"qux"}})
}

func testAuthErrorsForCatalog(t *testing.T, s test.Setup) {
	ctx := t.Context()

	// without token, expect auth challenge
	s.RespondTo(ctx, "GET /v2/_catalog").
		ExpectHeader(t, "Content-Type", "application/json").
		ExpectHeader(t, "Www-Authenticate", `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="registry:catalog:*"`).
		ExpectJSON(t, http.StatusUnauthorized, test.ErrorCode(keppel.ErrUnauthorized))

	// with token for wrong scope, expect Forbidden and renewed auth challenge
	tokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull")
	s.RespondTo(ctx, "GET /v2/_catalog", httptest.WithHeaders(tokenHeaders)).
		ExpectHeader(t, "Content-Type", "application/json").
		ExpectHeader(t, "Www-Authenticate", `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="registry:catalog:*",error="insufficient_scope"`).
		//NOTE: Docker Hub (https://registry-1.docker.io) sends UNAUTHORIZED here, but DENIED is more logical.
		ExpectJSON(t, http.StatusUnauthorized, test.ErrorCode(keppel.ErrDenied))

	// without token, expect auth challenge (test for domain-remapped API)
	s.RespondTo(ctx, "GET /v2/_catalog", httptest.WithHeaders(http.Header{
		"X-Forwarded-Host":  {"test1.registry.example.org"},
		"X-Forwarded-Proto": {"https"},
	})).
		ExpectHeader(t, "Content-Type", "application/json").
		ExpectHeader(t, "Www-Authenticate", `Bearer realm="https://test1.registry.example.org/keppel/v1/auth",service="test1.registry.example.org",scope="registry:catalog:*"`).
		ExpectJSON(t, http.StatusUnauthorized, test.ErrorCode(keppel.ErrUnauthorized))
}

func testNoCatalogOnAnycast(t *testing.T, s test.Setup) {
	ctx := t.Context()
	tokenHeaders := s.GetAnycastTokenHeaders(t, "registry:catalog:*")
	s.RespondTo(ctx, "GET /v2/_catalog", httptest.WithHeaders(tokenHeaders)).
		ExpectJSON(t, http.StatusMethodNotAllowed, test.ErrorCode(keppel.ErrUnsupported))
}
