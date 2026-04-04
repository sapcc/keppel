// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/majewsky/gg/jsonmatch"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/httptest"

	"github.com/sapcc/keppel/internal/test"
)

func TestReferrersApi(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		ctx := t.Context()
		h := s.Handler
		token := s.GetToken(t, "repository:test1/foo:pull,push")

		image := test.GenerateOCIImage(test.OCIArgs{
			ConfigMediaType: imgspecv1.MediaTypeImageManifest,
		})
		image.MustUpload(t, s, fooRepoRef, "latest")

		subjectManifest := test.GenerateOCIImage(test.OCIArgs{
			ConfigMediaType: imgspecv1.MediaTypeImageManifest,
			SubjectDigest:   image.Manifest.Digest,
		})
		subjectManifest.MustUpload(t, s, fooRepoRef, strings.ReplaceAll(image.Manifest.Digest.String(), ":", "-"))

		resp := h.RespondTo(ctx, "GET /v2/test1/foo/referrers/"+image.Manifest.Digest.String(),
			httptest.WithHeader("Authorization", "Bearer "+token))
		resp.ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"schemaVersion": 2,
			"mediaType":     "application/vnd.oci.image.index.v1+json",
			"manifests": jsonmatch.Array{jsonmatch.Object{
				"artifactType": imgspecv1.MediaTypeImageManifest,
				"digest":       subjectManifest.Manifest.Digest.String(),
				"mediaType":    imgspecv1.MediaTypeImageManifest,
				"size":         subjectManifest.SizeBytes(),
			}},
		})
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
	})
}
