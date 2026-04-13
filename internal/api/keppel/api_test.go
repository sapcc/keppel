// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppelv1_test

import (
	"net/http"
	"testing"

	"github.com/majewsky/gg/jsonmatch"
	"github.com/sapcc/go-bits/httptest"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestAlternativeAuthSchemes(t *testing.T) {
	s := test.NewSetup(t,
		test.WithKeppelAPI,
		test.WithAccount(models.Account{Name: "test1", AuthTenantID: "tenant1"}),
		test.WithRepo(models.Repository{Name: "foo", AccountName: "test1"}),
	)
	h := s.Handler
	ctx := t.Context()

	// test anonymous auth: fails without RBAC policy, succeeds with RBAC policy
	h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories/foo/_manifests").
		ExpectHeader(t, "Www-Authenticate",
			`Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="repository:test1/foo:pull"`).
		ExpectText(t, http.StatusForbidden, "no bearer token found in request headers\n")

	test.MustExec(t, s.DB, `UPDATE accounts SET rbac_policies_json = $2 WHERE name = $1`, "test1",
		test.ToJSON([]keppel.RBACPolicy{{
			RepositoryPattern: "foo",
			Permissions:       []keppel.RBACPermission{keppel.RBACAnonymousPullPermission},
		}}),
	)
	h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories/foo/_manifests").
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{"manifests": []jsonmatch.Object{}})
	test.MustExec(t, s.DB, `UPDATE accounts SET rbac_policies_json = $2 WHERE name = $1`, "test1", "")

	// test bearer token auth: obtain a bearer token on the Auth API while
	// authenticating with Keppel API Auth, then use the bearer token on the
	// Keppel API
	var tokenData struct {
		Token string `json:"token"`
	}
	h.RespondTo(ctx, "GET /keppel/v1/auth?service=registry.example.org&scope=repository:test1/foo:pull",
		withPerms("view:tenant1,pull:tenant1"),
	).CaptureJSON(&tokenData).ExpectStatus(t, http.StatusOK)
	h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories/foo/_manifests",
		httptest.WithHeader("Authorization", "Bearer "+tokenData.Token),
	).ExpectJSON(t, http.StatusOK, jsonmatch.Object{"manifests": []jsonmatch.Object{}})
}
