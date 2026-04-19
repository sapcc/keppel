// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"bytes"
	"net/http"
	"strconv"
	"testing"

	"github.com/sapcc/go-bits/httptest"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestRegistryAPIDomainRemap(t *testing.T) {
	ctx := t.Context()

	// test generic Registry API endpoints with request URLs having the account name in the hostname instead of in the path
	testWithPrimary(t, nil, func(s test.Setup) {
		// without token, expect auth challenge
		s.RespondTo(ctx, "GET /v2/", httptest.WithHeaders(http.Header{
			"X-Forwarded-Host":  {"test1.registry.example.org"},
			"X-Forwarded-Proto": {"https"},
		})).
			ExpectHeader(t, "Www-Authenticate", `Bearer realm="https://test1.registry.example.org/keppel/v1/auth",service="test1.registry.example.org"`).
			ExpectJSON(t, http.StatusUnauthorized, test.ErrorCode(keppel.ErrUnauthorized))

		// with token, expect status code 200
		tokenHeaders := s.GetDomainRemappedTokenHeaders(t, "test1" /*, no scopes */)
		s.RespondTo(ctx, "GET /v2/", httptest.WithHeaders(tokenHeaders)).
			ExpectStatus(t, http.StatusOK)
	})
}

func TestBlobAPIDomainRemap(t *testing.T) {
	ctx := t.Context()
	blob := test.NewBytes([]byte("just some random data"))

	// test blob API with request URLs having the account name in the hostname instead of in the path
	testWithPrimary(t, nil, func(s test.Setup) {
		tokenHeaders := s.GetDomainRemappedTokenHeaders(t, "test1", "repository:foo:pull,push")

		// test upload
		s.RespondTo(ctx, "POST /v2/foo/blobs/uploads/?digest="+blob.Digest.String(),
			httptest.WithHeaders(tokenHeaders),
			httptest.WithHeader("Content-Length", strconv.Itoa(len(blob.Contents))),
			httptest.WithHeader("Content-Type", blob.MediaType),
			httptest.WithBody(bytes.NewReader(blob.Contents)),
		).
			ExpectHeader(t, "Content-Length", "0").
			ExpectHeader(t, "Location", "/v2/foo/blobs/"+blob.Digest.String()).
			ExpectStatus(t, http.StatusCreated)

		// test download
		s.RespondTo(ctx, "GET /v2/foo/blobs/"+blob.Digest.String(),
			httptest.WithHeaders(tokenHeaders),
		).Expect(containsBlob(t, blob))
	})
}

func TestManifestAPIDomainRemap(t *testing.T) {
	ctx := t.Context()
	image := test.GenerateImage( /* no layers */ )

	// test manifest API with request URLs having the account name in the hostname instead of in the path
	testWithPrimary(t, nil, func(s test.Setup) {
		tokenHeaders := s.GetDomainRemappedTokenHeaders(t, "test1", "repository:foo:pull,push")
		image.Config.MustUpload(t, s, fooRepoRef)

		// test upload
		s.RespondTo(ctx, "PUT /v2/foo/manifests/"+image.Manifest.Digest.String(),
			httptest.WithHeaders(tokenHeaders),
			uploadingManifest(image.Manifest),
		).ExpectStatus(t, http.StatusCreated)

		// test download
		s.RespondTo(ctx, "GET /v2/foo/manifests/"+image.Manifest.Digest.String(),
			httptest.WithHeaders(tokenHeaders),
		).Expect(containsManifest(t, image.Manifest))
	})
}
