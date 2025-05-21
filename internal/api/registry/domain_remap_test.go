// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/sapcc/go-bits/assert"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestRegistryAPIDomainRemap(t *testing.T) {
	// test generic Registry API endpoints with request URLs having the account name in the hostname instead of in the path
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler

		// without token, expect auth challenge
		assert.HTTPRequest{
			Method: "GET",
			Path:   "/v2/",
			Header: map[string]string{
				"X-Forwarded-Host":  "test1.registry.example.org",
				"X-Forwarded-Proto": "https",
			},
			ExpectStatus: http.StatusUnauthorized,
			ExpectHeader: map[string]string{
				test.VersionHeaderKey: test.VersionHeaderValue,
				"Www-Authenticate":    `Bearer realm="https://test1.registry.example.org/keppel/v1/auth",service="test1.registry.example.org"`,
			},
			ExpectBody: test.ErrorCode(keppel.ErrUnauthorized),
		}.Check(t, h)

		// with token, expect status code 200
		token := s.GetDomainRemappedToken(t, "test1" /*, no scopes */)
		assert.HTTPRequest{
			Method: "GET",
			Path:   "/v2/",
			Header: map[string]string{
				"Authorization":     "Bearer " + token,
				"X-Forwarded-Host":  "test1.registry.example.org",
				"X-Forwarded-Proto": "https",
			},
			ExpectStatus: http.StatusOK,
			ExpectHeader: test.VersionHeader,
		}.Check(t, h)
	})
}

func TestBlobAPIDomainRemap(t *testing.T) {
	// test blob API with request URLs having the account name in the hostname instead of in the path
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler
		token := s.GetDomainRemappedToken(t, "test1", "repository:foo:pull,push")

		blob := test.NewBytes([]byte("just some random data"))

		// test upload
		assert.HTTPRequest{
			Method: "POST",
			Path:   "/v2/foo/blobs/uploads/?digest=" + blob.Digest.String(),
			Header: map[string]string{
				"Authorization":     "Bearer " + token,
				"Content-Length":    strconv.Itoa(len(blob.Contents)),
				"Content-Type":      "application/octet-stream",
				"X-Forwarded-Host":  "test1.registry.example.org",
				"X-Forwarded-Proto": "https",
			},
			Body:         assert.ByteData(blob.Contents),
			ExpectStatus: http.StatusCreated,
			ExpectHeader: map[string]string{
				test.VersionHeaderKey: test.VersionHeaderValue,
				"Content-Length":      "0",
				"Location":            "/v2/foo/blobs/" + blob.Digest.String(),
			},
		}.Check(t, h)

		// test download
		assert.HTTPRequest{
			Method: "GET",
			Path:   "/v2/foo/blobs/" + blob.Digest.String(),
			Header: map[string]string{
				"Authorization":     "Bearer " + token,
				"X-Forwarded-Host":  "test1.registry.example.org",
				"X-Forwarded-Proto": "https",
			},
			ExpectStatus: http.StatusOK,
			ExpectBody:   assert.ByteData(blob.Contents),
			ExpectHeader: map[string]string{
				test.VersionHeaderKey: test.VersionHeaderValue,
				"Content-Length":      strconv.Itoa(len(blob.Contents)),
				"Content-Type":        "application/octet-stream",
			},
		}.Check(t, h)
	})
}

func TestManifestAPIDomainRemap(t *testing.T) {
	image := test.GenerateImage( /* no layers */ )

	// test manifest API with request URLs having the account name in the hostname instead of in the path
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler
		token := s.GetDomainRemappedToken(t, "test1", "repository:foo:pull,push")
		image.Config.MustUpload(t, s, fooRepoRef)

		// test upload
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/v2/foo/manifests/" + image.Manifest.Digest.String(),
			Header: map[string]string{
				"Authorization":     "Bearer " + token,
				"Content-Type":      image.Manifest.MediaType,
				"X-Forwarded-Host":  "test1.registry.example.org",
				"X-Forwarded-Proto": "https",
			},
			Body:         assert.ByteData(image.Manifest.Contents),
			ExpectStatus: http.StatusCreated,
			ExpectHeader: test.VersionHeader,
		}.Check(t, h)

		// test download
		assert.HTTPRequest{
			Method: "GET",
			Path:   "/v2/foo/manifests/" + image.Manifest.Digest.String(),
			Header: map[string]string{
				"Authorization":     "Bearer " + token,
				"X-Forwarded-Host":  "test1.registry.example.org",
				"X-Forwarded-Proto": "https",
			},
			ExpectStatus: http.StatusOK,
			ExpectBody:   assert.ByteData(image.Manifest.Contents),
			ExpectHeader: map[string]string{
				test.VersionHeaderKey: test.VersionHeaderValue,
				"Content-Length":      strconv.Itoa(len(image.Manifest.Contents)),
				"Content-Type":        image.Manifest.MediaType,
			},
		}.Check(t, h)
	})
}
