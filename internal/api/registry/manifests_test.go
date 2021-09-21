/******************************************************************************
*
*  Copyright 2020 SAP SE
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
	"encoding/json"
	"net/http"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/hermes/pkg/cadf"
	"github.com/sapcc/keppel/internal/clair"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestImageManifestLifecycle(t *testing.T) {
	image := test.GenerateImage( /* no layers */ )

	for _, tagName := range []string{"latest", ""} {
		testWithPrimary(t, nil, func(s test.Setup) {
			h := s.Handler
			token := s.GetToken(t, "repository:test1/foo:pull,push")
			readOnlyToken := s.GetToken(t, "repository:test1/foo:pull")
			otherRepoToken := s.GetToken(t, "repository:test1/bar:pull,push")
			deleteToken := s.GetToken(t, "repository:test1/foo:delete")

			//on the API, we either reference the tag name (if uploading with tag) or the digest (if uploading without tag)
			ref := tagName
			if tagName == "" {
				ref = image.Manifest.Digest.String()
			}

			//repo does not exist before we first push to it
			for _, method := range []string{"GET", "HEAD"} {
				assert.HTTPRequest{
					Method:       method,
					Path:         "/v2/test1/foo/manifests/" + ref,
					Header:       map[string]string{"Authorization": "Bearer " + readOnlyToken},
					ExpectStatus: http.StatusNotFound,
					ExpectHeader: test.VersionHeader,
					ExpectBody:   bodyForMethod(method, test.ErrorCode(keppel.ErrNameUnknown)),
				}.Check(t, h)
			}

			//and even if it does...
			_, err := keppel.FindOrCreateRepository(s.DB, "foo", keppel.Account{Name: "test1"})
			if err != nil {
				t.Fatal(err.Error())
			}
			//...the manifest does not exist before it is pushed
			for _, method := range []string{"GET", "HEAD"} {
				assert.HTTPRequest{
					Method:       method,
					Path:         "/v2/test1/foo/manifests/" + ref,
					Header:       map[string]string{"Authorization": "Bearer " + readOnlyToken},
					ExpectStatus: http.StatusNotFound,
					ExpectHeader: test.VersionHeader,
					ExpectBody:   bodyForMethod(method, test.ErrorCode(keppel.ErrManifestUnknown)),
				}.Check(t, h)
			}

			//PUT failure case: cannot push with read-only token
			assert.HTTPRequest{
				Method: "PUT",
				Path:   "/v2/test1/foo/manifests/" + ref,
				Header: map[string]string{
					"Authorization": "Bearer " + readOnlyToken,
					"Content-Type":  image.Manifest.MediaType,
				},
				Body:         assert.ByteData(image.Manifest.Contents),
				ExpectStatus: http.StatusUnauthorized,
				ExpectBody:   test.ErrorCode(keppel.ErrDenied),
			}.Check(t, h)

			//PUT failure case: cannot push while account is in maintenance
			testWithAccountInMaintenance(t, s.DB, "test1", func() {
				assert.HTTPRequest{
					Method: "PUT",
					Path:   "/v2/test1/foo/manifests/" + ref,
					Header: map[string]string{
						"Authorization": "Bearer " + token,
						"Content-Type":  image.Manifest.MediaType,
					},
					Body:         assert.ByteData(image.Manifest.Contents),
					ExpectStatus: http.StatusMethodNotAllowed,
					ExpectBody: test.ErrorCodeWithMessage{
						Code:    keppel.ErrUnsupported,
						Message: "account is in maintenance",
					},
				}.Check(t, h)
			})

			//PUT failure case: malformed manifest
			assert.HTTPRequest{
				Method: "PUT",
				Path:   "/v2/test1/foo/manifests/" + ref,
				Header: map[string]string{
					"Authorization": "Bearer " + token,
					"Content-Type":  image.Manifest.MediaType,
				},
				Body:         assert.ByteData(append([]byte("wtf"), image.Manifest.Contents...)),
				ExpectStatus: http.StatusBadRequest,
				ExpectBody:   test.ErrorCode(keppel.ErrManifestInvalid),
			}.Check(t, h)

			//PUT failure case: wrong digest
			assert.HTTPRequest{
				Method: "PUT",
				Path:   "/v2/test1/foo/manifests/sha256:" + sha256Of([]byte("something else")),
				Header: map[string]string{
					"Authorization": "Bearer " + token,
					"Content-Type":  image.Manifest.MediaType,
				},
				Body:         assert.ByteData(image.Manifest.Contents),
				ExpectStatus: http.StatusBadRequest,
				ExpectBody:   test.ErrorCode(keppel.ErrDigestInvalid),
			}.Check(t, h)

			//PUT failure case: cannot upload manifest if referenced blob is missing
			assert.HTTPRequest{
				Method: "PUT",
				Path:   "/v2/test1/foo/manifests/" + ref,
				Header: map[string]string{
					"Authorization": "Bearer " + token,
					"Content-Type":  image.Manifest.MediaType,
				},
				Body:         assert.ByteData(image.Manifest.Contents),
				ExpectStatus: http.StatusNotFound,
				ExpectBody:   test.ErrorCode(keppel.ErrManifestBlobUnknown),
			}.Check(t, h)

			//failed requests should not retain anything in the storage
			expectStorageEmpty(t, s.SD, s.DB)
			s.Auditor.ExpectEvents(t /*, nothing */)

			//PUT failure case: cannot upload manifest if referenced blob is uploaded, but
			//in the wrong repo
			image.Config.MustUpload(t, h, otherRepoToken, barRepoRef)
			assert.HTTPRequest{
				Method: "PUT",
				Path:   "/v2/test1/foo/manifests/" + ref,
				Header: map[string]string{
					"Authorization": "Bearer " + token,
					"Content-Type":  image.Manifest.MediaType,
				},
				Body:         assert.ByteData(image.Manifest.Contents),
				ExpectStatus: http.StatusNotFound,
				ExpectBody:   test.ErrorCode(keppel.ErrManifestBlobUnknown),
			}.Check(t, h)

			//PUT failure case: cannot upload manifest via the anycast API
			if currentlyWithAnycast {
				assert.HTTPRequest{
					Method: "PUT",
					Path:   "/v2/test1/foo/manifests/" + ref,
					Header: map[string]string{
						"Authorization":     "Bearer " + token,
						"Content-Type":      image.Manifest.MediaType,
						"X-Forwarded-Host":  s.Config.AnycastAPIPublicURL.Host,
						"X-Forwarded-Proto": s.Config.AnycastAPIPublicURL.Scheme,
					},
					Body:         assert.ByteData(image.Manifest.Contents),
					ExpectStatus: http.StatusMethodNotAllowed,
					ExpectHeader: test.VersionHeader,
					ExpectBody:   test.ErrorCode(keppel.ErrUnsupported),
				}.Check(t, h)
			}

			//there should still not be any manifests
			s.Auditor.ExpectEvents(t /*, nothing */)

			//PUT success case: upload manifest (and also the blob referenced by it);
			//each PUT is executed twice to test idempotency
			s.Clock.Step()
			easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/imagemanifest-001-before-upload-blob.sql")

			image.Config.MustUpload(t, h, token, fooRepoRef)
			image.Config.MustUpload(t, h, token, fooRepoRef)
			s.Clock.Step()
			easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/imagemanifest-002-after-upload-blob.sql")

			image.MustUpload(t, h, s.DB, token, fooRepoRef, tagName)
			image.MustUpload(t, h, s.DB, token, fooRepoRef, tagName)
			s.Clock.Step()
			if ref == "latest" {
				easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/imagemanifest-003-after-upload-manifest-by-tag.sql")
			} else {
				easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/imagemanifest-003-after-upload-manifest-by-digest.sql")
			}

			//we did two PUTs, but only the first one will be logged since the second one did not change anything
			auditEvents := []cadf.Event{{
				RequestPath: "/v2/test1/foo/manifests/" + ref,
				Action:      "create",
				Outcome:     "success",
				Reason:      test.CADFReasonOK,
				Target: cadf.Resource{
					TypeURI:   "docker-registry/account/repository/manifest",
					Name:      "test1/foo@" + image.Manifest.Digest.String(),
					ID:        image.Manifest.Digest.String(),
					ProjectID: authTenantID,
				},
			}}
			if ref != image.Manifest.Digest.String() {
				auditEvents = append(auditEvents, cadf.Event{
					RequestPath: "/v2/test1/foo/manifests/" + ref,
					Action:      "create",
					Outcome:     "success",
					Reason:      test.CADFReasonOK,
					Target: cadf.Resource{
						TypeURI:   "docker-registry/account/repository/tag",
						Name:      "test1/foo:" + ref,
						ID:        image.Manifest.Digest.String(),
						ProjectID: authTenantID,
					},
				})
			}
			s.Auditor.ExpectEvents(t, auditEvents...)

			//check GET/HEAD: manifest should now be available under the reference
			//where it was pushed to...
			expectManifestExists(t, h, readOnlyToken, "test1/foo", image.Manifest, ref, nil)
			//...and under its digest
			expectManifestExists(t, h, readOnlyToken, "test1/foo", image.Manifest, image.Manifest.Digest.String(), nil)

			//GET failure case: wrong scope
			assert.HTTPRequest{
				Method:       "GET",
				Path:         "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
				Header:       map[string]string{"Authorization": "Bearer " + otherRepoToken},
				ExpectStatus: http.StatusUnauthorized,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrDenied),
			}.Check(t, h)
			//^ NOTE: docker-registry sends UNAUTHORIZED (401) instead of DENIED (403)
			//        here, but 403 is more correct.

			//test GET via anycast
			if currentlyWithAnycast {
				testWithReplica(t, s, "on_first_use", func(firstPass bool, s2 test.Setup) {
					h2 := s2.Handler
					testAnycast(t, firstPass, s2.DB, func() {
						anycastToken := s.GetAnycastToken(t, "repository:test1/foo:pull")
						anycastHeaders := map[string]string{
							"X-Forwarded-Host":  s.Config.AnycastAPIPublicURL.Hostname(),
							"X-Forwarded-Proto": "https",
						}
						expectManifestExists(t, h, anycastToken, "test1/foo", image.Manifest, ref, anycastHeaders)
						expectManifestExists(t, h, anycastToken, "test1/foo", image.Manifest, image.Manifest.Digest.String(), anycastHeaders)
						expectManifestExists(t, h2, anycastToken, "test1/foo", image.Manifest, ref, anycastHeaders)
						expectManifestExists(t, h2, anycastToken, "test1/foo", image.Manifest, image.Manifest.Digest.String(), anycastHeaders)
					})
				})
			}

			//test display of custom headers during GET/HEAD
			_, err = s.DB.Exec(
				`UPDATE manifests SET vuln_status = $1 WHERE digest = $2`,
				clair.CleanSeverity, image.Manifest.Digest.String(),
			)
			if err != nil {
				t.Fatal(err.Error())
			}
			for _, method := range []string{"GET", "HEAD"} {
				assert.HTTPRequest{
					Method:       method,
					Path:         "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
					Header:       map[string]string{"Authorization": "Bearer " + readOnlyToken},
					ExpectStatus: http.StatusOK,
					ExpectHeader: map[string]string{
						test.VersionHeaderKey:           test.VersionHeaderValue,
						"X-Keppel-Vulnerability-Status": string(clair.CleanSeverity),
					},
				}.Check(t, h)
			}

			//DELETE failure case: no delete permission
			assert.HTTPRequest{
				Method:       "DELETE",
				Path:         "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
				Header:       map[string]string{"Authorization": "Bearer " + token},
				ExpectStatus: http.StatusUnauthorized,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrDenied),
			}.Check(t, h)

			//DELETE failure case: unknown manifest
			assert.HTTPRequest{
				Method:       "DELETE",
				Path:         "/v2/test1/foo/manifests/sha256:" + sha256Of([]byte("something else")),
				Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
				ExpectStatus: http.StatusNotFound,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrManifestUnknown),
			}.Check(t, h)

			//DELETE failure case: cannot delete blob while the manifest still exists in the DB
			assert.HTTPRequest{
				Method:       "DELETE",
				Path:         "/v2/test1/foo/blobs/" + image.Config.Digest.String(),
				Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
				ExpectStatus: http.StatusMethodNotAllowed,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrUnsupported),
			}.Check(t, h)

			//no deletes were successful yet, so...
			s.Auditor.ExpectEvents(t /*, nothing */)

			//DELETE success case
			assert.HTTPRequest{
				Method:       "DELETE",
				Path:         "/v2/test1/foo/manifests/" + ref,
				Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
				ExpectStatus: http.StatusAccepted,
				ExpectHeader: test.VersionHeader,
			}.Check(t, h)
			s.Clock.Step()
			if ref == "latest" {
				easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/imagemanifest-004-after-delete-tag.sql")
			} else {
				easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/imagemanifest-004-after-delete-manifest.sql")
			}

			//the DELETE will have logged an audit event
			event := cadf.Event{
				RequestPath: "/v2/test1/foo/manifests/" + ref,
				Action:      "delete",
				Outcome:     "success",
				Reason:      test.CADFReasonOK,
				Target: cadf.Resource{
					TypeURI:   "docker-registry/account/repository/manifest",
					Name:      "test1/foo@" + image.Manifest.Digest.String(),
					ID:        image.Manifest.Digest.String(),
					ProjectID: authTenantID,
				},
			}
			if ref == "latest" {
				event.Target.TypeURI = "docker-registry/account/repository/tag"
				event.Target.Name = "test1/foo:latest"
			}
			s.Auditor.ExpectEvents(t, event)
		})

	}
}

func bodyForMethod(method string, body assert.HTTPResponseBody) assert.HTTPResponseBody {
	if method == "HEAD" {
		return nil
	}
	return body
}

func TestImageListManifestLifecycle(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		//This test builds on TestImageManifestLifecycle and provides test coverage
		//for the parts of the manifest push workflow that check manifest-manifest
		//references. (We don't have those in plain images, only in image lists.)
		h := s.Handler
		token := s.GetToken(t, "repository:test1/foo:pull,push")
		deleteToken := s.GetToken(t, "repository:test1/foo:delete")

		//as a setup, upload two images and render a third image that's not uploaded
		image1 := test.GenerateImage(test.GenerateExampleLayer(1))
		image2 := test.GenerateImage(test.GenerateExampleLayer(2))
		image3 := test.GenerateImage(test.GenerateExampleLayer(3))
		s.Clock.Step()
		image1.MustUpload(t, h, s.DB, token, fooRepoRef, "first")
		s.Clock.Step()
		image2.MustUpload(t, h, s.DB, token, fooRepoRef, "second")
		s.Clock.Step()

		//PUT failure case: cannot upload image list manifest referencing missing manifests
		list1 := test.GenerateImageList(image1, image3)
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/v2/test1/foo/manifests/" + list1.Manifest.Digest.String(),
			Header: map[string]string{
				"Authorization": "Bearer " + token,
				"Content-Type":  list1.Manifest.MediaType,
			},
			Body:         assert.ByteData(list1.Manifest.Contents),
			ExpectStatus: http.StatusNotFound,
			ExpectBody:   test.ErrorCode(keppel.ErrManifestUnknown),
		}.Check(t, h)

		//PUT success case: upload image list manifest referencing available manifests
		list2 := test.GenerateImageList(image1, image2)
		list2.MustUpload(t, h, s.DB, token, fooRepoRef, "list")

		s.Clock.Step()
		easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/imagelistmanifest-001-after-upload-manifest.sql")

		//check GET for manifest list
		expectManifestExists(t, h, token, "test1/foo", list2.Manifest, "list", nil)

		//as a special case, GET on the manifest list returns the linux/amd64
		//manifest if only single-arch manifests are accepted by the client (this
		//behavior is somewhat dubious, but required for full compatibility with
		//existing clients)
		assert.HTTPRequest{
			Method: "GET",
			Path:   "/v2/test1/foo/manifests/" + list2.Manifest.Digest.String(),
			Header: map[string]string{
				"Authorization": "Bearer " + token,
				"Accept":        "application/vnd.docker.distribution.manifest.v2+json",
			},
			ExpectStatus: http.StatusTemporaryRedirect,
			ExpectHeader: map[string]string{
				test.VersionHeaderKey: test.VersionHeaderValue,
				"Location":            "/v2/test1/foo/manifests/" + image1.Manifest.Digest.String(),
			},
		}.Check(t, h)
		//but we return the whole list if at all possible
		expectManifestExists(t, h, token, "test1/foo", list2.Manifest, "list", map[string]string{
			"Accept": "application/vnd.docker.distribution.manifest.v2+json, application/vnd.docker.distribution.manifest.list.v2+json",
		})

		//DELETE success case
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/v2/test1/foo/manifests/" + list2.Manifest.Digest.String(),
			Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
			ExpectStatus: http.StatusAccepted,
			ExpectHeader: test.VersionHeader,
		}.Check(t, h)
		s.Clock.Step()
		easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/imagelistmanifest-002-after-delete-manifest.sql")
	})
}

func TestManifestQuotaExceeded(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler
		token := s.GetToken(t, "repository:test1/foo:pull,push")

		//as a setup, upload two images
		image1 := test.GenerateImage(test.GenerateExampleLayer(1))
		image2 := test.GenerateImage(test.GenerateExampleLayer(2))
		image1.MustUpload(t, h, s.DB, token, fooRepoRef, "first")
		image2.MustUpload(t, h, s.DB, token, fooRepoRef, "second")

		//set quota below usage
		_, err := s.DB.Exec(`UPDATE quotas SET manifests = $1`, 1)
		if err != nil {
			t.Fatal(err.Error())
		}

		quotaExceededMessage := test.ErrorCodeWithMessage{
			Code:    keppel.ErrDenied,
			Message: "manifest quota exceeded (quota = 1, usage = 2)",
		}

		//further blob uploads are not possible now
		assert.HTTPRequest{
			Method:       "POST",
			Path:         "/v2/test1/foo/blobs/uploads/",
			Header:       map[string]string{"Authorization": "Bearer " + token},
			ExpectStatus: http.StatusConflict,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   quotaExceededMessage,
		}.Check(t, h)

		//further manifest uploads are not possible now
		assert.HTTPRequest{
			Method:       "PUT",
			Path:         "/v2/test1/foo/manifests/anotherone",
			Header:       map[string]string{"Authorization": "Bearer " + token},
			Body:         assert.StringData("request body does not matter"),
			ExpectStatus: http.StatusConflict,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   quotaExceededMessage,
		}.Check(t, h)
	})
}

func TestManifestRequiredLabels(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler
		token := s.GetToken(t, "repository:test1/foo:pull,push")

		//setup test data: image config with labels "foo" and "bar"
		blob, err := test.NewBytesFromFile("fixtures/example-docker-image-config-with-labels.json")
		if err != nil {
			t.Fatal(err.Error())
		}

		//setup test data: manifest referencing that image config
		manifestData := assert.JSONObject{
			"schemaVersion": 2,
			"mediaType":     "application/vnd.docker.distribution.manifest.v2+json",
			"config": assert.JSONObject{
				"mediaType": "application/vnd.docker.container.image.v1+json",
				"size":      len(blob.Contents),
				"digest":    blob.Digest.String(),
			},
			"layers": []assert.JSONObject{},
		}

		//upload the config blob
		blob.MustUpload(t, h, token, fooRepoRef)

		//setup required labels on account for failure
		_, err = s.DB.Exec(
			`UPDATE accounts SET required_labels = $1 WHERE name = $2`,
			"foo,somethingelse,andalsothis", "test1",
		)
		if err != nil {
			t.Fatal(err.Error())
		}

		//manifest push should fail
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/v2/test1/foo/manifests/latest",
			Header: map[string]string{
				"Authorization": "Bearer " + token,
				"Content-Type":  "application/vnd.docker.distribution.manifest.v2+json",
			},
			Body:         manifestData,
			ExpectStatus: http.StatusBadRequest,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   test.ErrorCode(keppel.ErrManifestInvalid),
		}.Check(t, h)

		//setup required labels on account for success
		_, err = s.DB.Exec(
			`UPDATE accounts SET required_labels = $1 WHERE name = $2`,
			"foo,bar", "test1",
		)
		if err != nil {
			t.Fatal(err.Error())
		}

		//manifest push should succeed
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/v2/test1/foo/manifests/latest",
			Header: map[string]string{
				"Authorization": "Bearer " + token,
				"Content-Type":  "application/vnd.docker.distribution.manifest.v2+json",
			},
			Body:         manifestData,
			ExpectStatus: http.StatusCreated,
			ExpectHeader: test.VersionHeader,
		}.Check(t, h)

		//check that the labels_json field is populated correctly in the db
		var dbManifests []keppel.Manifest
		_, err = s.DB.Select(&dbManifests,
			`SELECT * FROM manifests`,
		)
		if err != nil {
			t.Fatal(err.Error())
		}
		if len(dbManifests) != 1 {
			t.Fatalf("expected 1 manifest entry in db, got: %d", len(dbManifests))
		}

		var actual map[string]string
		err = json.Unmarshal([]byte(dbManifests[0].LabelsJSON), &actual)
		if err != nil {
			t.Fatal(err.Error())
		}
		expected := map[string]string{
			"bar": "is there",
			"foo": "is there",
		}
		assert.DeepEqual(t, "labels_json", actual, expected)
	})
}

func TestImageManifestWrongBlobSize(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler
		token := s.GetToken(t, "repository:test1/foo:pull,push")

		//generate an image that references a layer, but the reference includes the wrong layer size
		layer := test.GenerateExampleLayer(1)
		layer.MustUpload(t, h, token, fooRepoRef)

		layer.Contents = append(layer.Contents, []byte("something")...)
		image := test.GenerateImage(layer)
		image.Config.MustUpload(t, h, token, fooRepoRef)

		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/v2/test1/foo/manifests/latest",
			Header: map[string]string{
				"Authorization": "Bearer " + token,
				"Content-Type":  image.Manifest.MediaType,
			},
			Body:         assert.ByteData(image.Manifest.Contents),
			ExpectStatus: http.StatusBadRequest,
			ExpectBody:   test.ErrorCode(keppel.ErrManifestInvalid),
		}.Check(t, h)
	})
}
