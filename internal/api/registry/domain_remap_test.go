// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"bytes"
	"net/http"
	"strconv"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/httptest"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestRegistryAPIDomainRemap(t *testing.T) {
	// test generic Registry API endpoints with request URLs having the account name in the hostname instead of in the path
	testWithPrimary(t, nil, func(s test.Setup) {
		ctx := t.Context()
		h := s.Handler

		// without token, expect auth challenge
		resp := h.RespondTo(ctx, "GET /v2/",
			httptest.WithHeader("X-Forwarded-Host", "test1.registry.example.org"),
			httptest.WithHeader("X-Forwarded-Proto", "https"))
		resp.ExpectJSON(t, http.StatusUnauthorized, errCodeJSON(keppel.ErrUnauthorized))
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
		assert.Equal(t, resp.Header().Get("Www-Authenticate"), `Bearer realm="https://test1.registry.example.org/keppel/v1/auth",service="test1.registry.example.org"`)

		// with token, expect status code 200
		token := s.GetDomainRemappedToken(t, "test1" /*, no scopes */)
		resp = h.RespondTo(ctx, "GET /v2/",
			httptest.WithHeader("Authorization", "Bearer "+token),
			httptest.WithHeader("X-Forwarded-Host", "test1.registry.example.org"),
			httptest.WithHeader("X-Forwarded-Proto", "https"))
		resp.ExpectStatus(t, http.StatusOK)
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
	})
}

func TestBlobAPIDomainRemap(t *testing.T) {
	// test blob API with request URLs having the account name in the hostname instead of in the path
	testWithPrimary(t, nil, func(s test.Setup) {
		ctx := t.Context()
		h := s.Handler
		token := s.GetDomainRemappedToken(t, "test1", "repository:foo:pull,push")

		blob := test.NewBytes([]byte("just some random data"))

		// test upload
		resp := h.RespondTo(ctx, "POST /v2/foo/blobs/uploads/?digest="+blob.Digest.String(),
			httptest.WithHeader("Authorization", "Bearer "+token),
			httptest.WithHeader("Content-Length", strconv.Itoa(len(blob.Contents))),
			httptest.WithHeader("Content-Type", "application/octet-stream"),
			httptest.WithHeader("X-Forwarded-Host", "test1.registry.example.org"),
			httptest.WithHeader("X-Forwarded-Proto", "https"),
			httptest.WithBody(bytes.NewReader(blob.Contents)))
		resp.ExpectStatus(t, http.StatusCreated)
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
		assert.Equal(t, resp.Header().Get("Content-Length"), "0")
		assert.Equal(t, resp.Header().Get("Location"), "/v2/foo/blobs/"+blob.Digest.String())

		// test download
		resp = h.RespondTo(ctx, "GET /v2/foo/blobs/"+blob.Digest.String(),
			httptest.WithHeader("Authorization", "Bearer "+token),
			httptest.WithHeader("X-Forwarded-Host", "test1.registry.example.org"),
			httptest.WithHeader("X-Forwarded-Proto", "https"))
		resp.ExpectStatus(t, http.StatusOK)
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
		assert.Equal(t, resp.Header().Get("Content-Length"), strconv.Itoa(len(blob.Contents)))
		assert.Equal(t, resp.Header().Get("Content-Type"), "application/octet-stream")
		assert.DeepEqual(t, "body", resp.BodyBytes(), blob.Contents)
	})
}

func TestManifestAPIDomainRemap(t *testing.T) {
	image := test.GenerateImage( /* no layers */ )

	// test manifest API with request URLs having the account name in the hostname instead of in the path
	testWithPrimary(t, nil, func(s test.Setup) {
		ctx := t.Context()
		h := s.Handler
		token := s.GetDomainRemappedToken(t, "test1", "repository:foo:pull,push")
		image.Config.MustUpload(t, s, fooRepoRef)

		// test upload
		resp := h.RespondTo(ctx, "PUT /v2/foo/manifests/"+image.Manifest.Digest.String(),
			httptest.WithHeader("Authorization", "Bearer "+token),
			httptest.WithHeader("Content-Type", image.Manifest.MediaType),
			httptest.WithHeader("X-Forwarded-Host", "test1.registry.example.org"),
			httptest.WithHeader("X-Forwarded-Proto", "https"),
			httptest.WithBody(bytes.NewReader(image.Manifest.Contents)))
		resp.ExpectStatus(t, http.StatusCreated)
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)

		// test download
		resp = h.RespondTo(ctx, "GET /v2/foo/manifests/"+image.Manifest.Digest.String(),
			httptest.WithHeader("Authorization", "Bearer "+token),
			httptest.WithHeader("X-Forwarded-Host", "test1.registry.example.org"),
			httptest.WithHeader("X-Forwarded-Proto", "https"))
		resp.ExpectStatus(t, http.StatusOK)
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
		assert.Equal(t, resp.Header().Get("Content-Length"), strconv.Itoa(len(image.Manifest.Contents)))
		assert.Equal(t, resp.Header().Get("Content-Type"), image.Manifest.MediaType)
		assert.DeepEqual(t, "body", resp.BodyBytes(), image.Manifest.Contents)
	})
}
