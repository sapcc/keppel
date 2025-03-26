/******************************************************************************
*
*  Copyright 2025 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package registryv2_test

import (
	"net/http"
	"strings"
	"testing"

	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sapcc/go-bits/assert"

	"github.com/sapcc/keppel/internal/test"
)

func TestReferrersApi(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
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

		assert.HTTPRequest{
			Method: "GET",
			Path:   "/v2/test1/foo/referrers/" + image.Manifest.Digest.String(),
			Header: map[string]string{
				"Authorization": "Bearer " + token,
			},
			ExpectBody: assert.JSONObject{
				"schemaVersion": 2,
				"mediaType":     "application/vnd.oci.image.index.v1+json",
				"manifests": []assert.JSONObject{{
					"artifactType": "application/vnd.oci.image.manifest.v1+json",
					"digest":       subjectManifest.Manifest.Digest.String(),
					"mediaType":    imgspecv1.MediaTypeImageManifest,
					"size":         subjectManifest.SizeBytes(),
				}},
			},
			ExpectStatus: http.StatusOK,
			ExpectHeader: test.VersionHeader,
		}.Check(t, h)
	})
}
