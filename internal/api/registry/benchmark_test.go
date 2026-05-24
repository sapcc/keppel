// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/sapcc/go-bits/httptest"

	"github.com/sapcc/keppel/internal/test"
)

// This benchmark covers only the operations that are most commonly executed.
// These operations should be considered with the highest priority when changing
// performance-relevant code.
func BenchmarkImportantReadOperations(b *testing.B) {
	testWithPrimary(b, nil, func(s test.Setup) {
		// run this only once
		if currentlyWithAnycast {
			return
		}

		image := test.GenerateImage( /* no layers */ )
		image.MustUpload(b, s, fooRepoRef, "latest")
		readOnlyTokenHeaders := s.GetTokenHeaders(b, "repository:test1/foo:pull")

		// do not propagate b.Context() into HTTP handler; benchmarks and tests
		// tend to have a very deeply nested context chain, which would skew
		// performance measurements in a way that is not indicative of non-test scenarios
		noctx := context.Background()

		getManifestMethodAndPath := "GET /v2/test1/foo/manifests/" + image.Manifest.Digest.String()
		b.Run("GetManifest", func(b *testing.B) {
			for b.Loop() {
				s.RespondTo(noctx, getManifestMethodAndPath,
					httptest.WithHeaders(readOnlyTokenHeaders),
				).ExpectStatus(b, http.StatusOK)
			}
		})

		getBlobMethodAndPath := "GET /v2/test1/foo/blobs/" + image.Config.Digest.String()
		b.Run("GetBlob", func(b *testing.B) {
			for b.Loop() {
				s.RespondTo(noctx, getBlobMethodAndPath,
					httptest.WithHeaders(readOnlyTokenHeaders),
				).ExpectStatus(b, http.StatusOK)
			}
		})

		b.Run("ListTags", func(b *testing.B) {
			for b.Loop() {
				s.RespondTo(noctx, "GET /v2/test1/foo/tags/list",
					httptest.WithHeaders(readOnlyTokenHeaders),
				).ExpectStatus(b, http.StatusOK)
			}
		})
	})
}
