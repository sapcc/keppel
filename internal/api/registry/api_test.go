// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/majewsky/gg/jsonmatch"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/httptest"
	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

			httptest.WithHeader("Authorization", "keppel"),
			httptest.WithHeader("X-Test-Perms", "view:test1authtenant"))
		resp.ExpectStatus(t, http.StatusOK)
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)

		// test scopeless endpoint: failure case ("Authorization: keppel" means that
		// we want Keppel API auth, but then we don't pass the respective headers,
		// so we get a 401; we do not get an auth challenge since Keppel API auth
		// does not work with auth challenges)
		resp = h.RespondTo(ctx, "GET /v2/",
			httptest.WithHeader("Authorization", "keppel"))
		resp.ExpectStatus(t, http.StatusUnauthorized)
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
		assert.Equal(t, resp.Header().Get("Www-Authenticate"), "")

		// test catalog endpoint: happy case
		resp = h.RespondTo(ctx, "GET /v2/_catalog",
			httptest.WithHeader("Authorization", "keppel"),
			httptest.WithHeader("X-Test-Perms", "view:test1authtenant"))
		resp.ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"repositories": []string{"test1/foo"},
		})
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)

		// test catalog endpoint: "failure" case (no access to account -> empty list)
		resp = h.RespondTo(ctx, "GET /v2/_catalog",
			httptest.WithHeader("Authorization", "keppel"),
			httptest.WithHeader("X-Test-Perms", "view:someothertenant"))
		resp.ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"repositories": []string{},
		})
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)

		// test repository-scoped endpoint: happy case
		resp = h.RespondTo(ctx, "GET /v2/test1/foo/manifests/"+image.Manifest.Digest.String(),
			httptest.WithHeader("Authorization", "keppel"),
			httptest.WithHeader("X-Test-Perms", "view:test1authtenant,pull:test1authtenant"))
		resp.ExpectStatus(t, http.StatusOK)
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
		assert.DeepEqual(t, "body", resp.BodyBytes(), image.Manifest.Contents)

		// test repository-scoped endpoint: failure case (no pull permission)
		resp = h.RespondTo(ctx, "GET /v2/test1/foo/manifests/"+image.Manifest.Digest.String(),
			httptest.WithHeader("Authorization", "keppel"),
			httptest.WithHeader("X-Test-Perms", "view:test1authtenant"))
		resp.ExpectStatus(t, http.StatusUnauthorized)
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
		assert.Equal(t, resp.Header().Get("Www-Authenticate"), "") // Keppel API auth does not use auth challenges
	})
}

func TestAPIAuthNotGrantingAnyScopes(t *testing.T) {
	ctx := t.Context()
	testWithPrimary(t, nil, func(s test.Setup) {
		// any endpoint, when not provided with a token, should respond with 401
		// and challenge us to get one (unless RBAC policies for anonymous access are set up)
		resp := s.Handler.RespondTo(ctx, "GET /v2/test1/foo/manifests/latest")
		resp.ExpectStatus(t, http.StatusUnauthorized)
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
		assert.Equal(t, resp.Header().Get("Www-Authenticate"),
			`Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="repository:test1/foo:pull"`,
		)

		// any endpoint, when provided with a token that does not grant the right scopes,
		// should respond with 403 (though actually it's 401 for bug-for-bug compatibility with Docker Hub)
		token := s.GetToken(t /*, no scopes */)
		deniedMessage := jsonmatch.Object{
			"errors": []jsonmatch.Object{{
				"code":    keppel.ErrDenied,
				"message": "token does not cover scope repository:test1/foo:pull",
				"detail":  nil,
			}},
		}
		s.Handler.RespondTo(ctx, "GET /v2/test1/foo/manifests/latest", httptest.WithHeader("Authorization", "Bearer "+token)).
			ExpectJSON(t, http.StatusUnauthorized, deniedMessage)

		// same test, but with an anonymous user
		//
		// This is the actually interesting part of this test. We had a bug here where this specific
		// case reported "no bearer token found in request headers" which is objectively untrue.
		token = must.ReturnT(auth.Authorization{
			UserIdentity: auth.AnonymousUserIdentity,
			Audience:     auth.Audience{IsAnycast: false},
			ScopeSet:     auth.ScopeSet{},
		}.IssueToken(s.Config))(t).Token
		s.Handler.RespondTo(ctx, "GET /v2/test1/foo/manifests/latest", httptest.WithHeader("Authorization", "Bearer "+token)).
			ExpectJSON(t, http.StatusUnauthorized, deniedMessage)
	})
}
