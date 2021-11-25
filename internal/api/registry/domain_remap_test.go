/*******************************************************************************
*
* Copyright 2021 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package registryv2_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/keppel/internal/test"
)

func TestBlobAPIDomainRemap(t *testing.T) {
	//test blob API with request URLs having the account name in the hostname instead of in the path
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler
		token := s.GetToken(t, "repository:test1/foo:pull,push")

		blob := test.NewBytes([]byte("just some random data"))

		//test upload
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

		//test download
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

	//test manifest API with request URLs having the account name in the hostname instead of in the path
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler
		token := s.GetToken(t, "repository:test1/foo:pull,push")
		image.Config.MustUpload(t, s, fooRepoRef)

		//test upload
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

		//test download
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

		//test that only Registry API is allowed on the domain-remapped hostname
		assert.HTTPRequest{
			Method: "GET",
			Path:   "/keppel/v1/accounts/test1/repositories/foo/_manifests",
			Header: map[string]string{
				"Authorization":     "Bearer " + token,
				"X-Forwarded-Host":  "test1.registry.example.org",
				"X-Forwarded-Proto": "https",
			},
			ExpectStatus: http.StatusBadRequest,
			ExpectBody:   assert.StringData("request path invalid for this hostname\n"),
		}.Check(t, h)
	})
}
