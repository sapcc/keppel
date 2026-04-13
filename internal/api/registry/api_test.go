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

func TestVersionCheckEndpoint(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler

		// without token, expect auth challenge
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/v2/",
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

		// with token, expect status code 200
		tokenHeaders := s.GetTokenHeaders(t /*, no scopes */)
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/v2/",
			Header:       test.FlattenHeaders(tokenHeaders),
			ExpectStatus: http.StatusOK,
			ExpectHeader: test.VersionHeader,
		}.Check(t, h)
	})
}

func TestKeppelAPIAuth(t *testing.T) {
	// All the other tests use the conventional auth method using bearer tokens.
	// This test provides test coverage for authenticating with the same
	// AuthDriver-dependent mechanism used by the Keppel API.
	testWithPrimary(t, nil, func(s test.Setup) {
		// upload a manifest for testing (using bearer tokens since all our test
		// helper functions use those)
		h := s.Handler
		image := test.GenerateImage(test.GenerateExampleLayer(1))
		s.Clock.StepBy(time.Second)
		image.MustUpload(t, s, fooRepoRef, "first")

		// test scopeless endpoint: happy case
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
		// test scopeless endpoint: failure case ("Authorization: keppel" means that
		// we want Keppel API auth, but then we don't pass the respective headers,
		// so we get a 401; we do not get an auth challenge since Keppel API auth
		// does not work with auth challenges)
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/v2/",
			Header:       map[string]string{"Authorization": "keppel"},
			ExpectStatus: http.StatusUnauthorized,
			ExpectHeader: map[string]string{
				test.VersionHeaderKey: test.VersionHeaderValue,
				"Www-Authenticate":    "",
			},
		}.Check(t, h)

		// test catalog endpoint: happy case
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
		// test catalog endpoint: "failure" case (no access to account -> empty list)
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

		// test repository-scoped endpoint: happy case
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
		// test repository-scoped endpoint: failure case (no pull permission)
		assert.HTTPRequest{
			Method: "GET",
			Path:   "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
			Header: map[string]string{
				"Authorization": "keppel",
				"X-Test-Perms":  "view:test1authtenant",
			},
			ExpectStatus: http.StatusUnauthorized,
			ExpectHeader: map[string]string{
				test.VersionHeaderKey: test.VersionHeaderValue,
				"Www-Authenticate":    "", // Keppel API auth does not use auth challenges
			},
		}.Check(t, h)
	})
}

func TestAPIAuthNotGrantingAnyScopes(t *testing.T) {
	ctx := t.Context()
	testWithPrimary(t, nil, func(s test.Setup) {
		// any endpoint, when not provided with a token, should respond with 401
		// and challenge us to get one (unless RBAC policies for anonymous access are set up)
		s.Handler.RespondTo(ctx, "GET /v2/test1/foo/manifests/latest").
			ExpectHeader(t, test.VersionHeaderKey, test.VersionHeaderValue).
			ExpectHeader(t, "Www-Authenticate",
				`Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="repository:test1/foo:pull"`).
			ExpectStatus(t, http.StatusUnauthorized)

		// any endpoint, when provided with a token that does not grant the right scopes,
		// should respond with 403 (though actually it's 401 for bug-for-bug compatibility with Docker Hub)
		tokenHeaders := s.GetTokenHeaders(t /*, no scopes */)
		deniedMessage := jsonmatch.Object{
			"errors": []jsonmatch.Object{{
				"code":    keppel.ErrDenied,
				"message": "token does not cover scope repository:test1/foo:pull",
				"detail":  nil,
			}},
		}
		s.Handler.RespondTo(ctx, "GET /v2/test1/foo/manifests/latest", httptest.WithHeaders(tokenHeaders)).
			ExpectJSON(t, http.StatusUnauthorized, deniedMessage)

		// same test, but with an anonymous user
		//
		// This is the actually interesting part of this test. We had a bug here where this specific
		// case reported "no bearer token found in request headers" which is objectively untrue.
		token := must.ReturnT(auth.Authorization{
			UserIdentity: auth.AnonymousUserIdentity,
			Audience:     auth.Audience{IsAnycast: false},
			ScopeSet:     auth.ScopeSet{},
		}.IssueToken(s.Config))(t).Token
		s.Handler.RespondTo(ctx, "GET /v2/test1/foo/manifests/latest", httptest.WithHeader("Authorization", "Bearer "+token)).
			ExpectJSON(t, http.StatusUnauthorized, deniedMessage)
	})
}
