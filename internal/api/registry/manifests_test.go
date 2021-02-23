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

package registryv2

import (
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

	for _, ref := range []string{"latest", image.Manifest.Digest.String()} {
		testWithPrimary(t, nil, func(h http.Handler, cfg keppel.Configuration, db *keppel.DB, ad *test.AuthDriver, sd *test.StorageDriver, fd *test.FederationDriver, clock *test.Clock, auditor *test.Auditor) {
			token := getToken(t, h, ad, "repository:test1/foo:pull,push",
				keppel.CanPullFromAccount,
				keppel.CanPushToAccount)
			readOnlyToken := getToken(t, h, ad, "repository:test1/foo:pull",
				keppel.CanPullFromAccount)
			otherRepoToken := getToken(t, h, ad, "repository:test1/bar:pull,push",
				keppel.CanPullFromAccount,
				keppel.CanPushToAccount)
			deleteToken := getToken(t, h, ad, "repository:test1/foo:delete",
				keppel.CanDeleteFromAccount)

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
			_, err := keppel.FindOrCreateRepository(db, "foo", keppel.Account{Name: "test1"})
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
			testWithAccountInMaintenance(t, db, "test1", func() {
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
			expectStorageEmpty(t, sd, db)
			auditor.ExpectEvents(t /*, nothing */)

			//PUT failure case: cannot upload manifest if referenced blob is uploaded, but
			//in the wrong repo
			uploadBlob(t, h, otherRepoToken, "test1/bar", image.Config)
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
			if currentScenario.WithAnycast {
				assert.HTTPRequest{
					Method: "PUT",
					Path:   "/v2/test1/foo/manifests/" + ref,
					Header: map[string]string{
						"Authorization":     "Bearer " + token,
						"Content-Type":      image.Manifest.MediaType,
						"X-Forwarded-Host":  cfg.AnycastAPIPublicURL.Host,
						"X-Forwarded-Proto": cfg.AnycastAPIPublicURL.Scheme,
					},
					Body:         assert.ByteData(image.Manifest.Contents),
					ExpectStatus: http.StatusMethodNotAllowed,
					ExpectHeader: test.VersionHeader,
					ExpectBody:   test.ErrorCode(keppel.ErrUnsupported),
				}.Check(t, h)
			}

			//there should still not be any manifests
			auditor.ExpectEvents(t /*, nothing */)

			//PUT success case: upload manifest (and also the blob referenced by it);
			//each PUT is executed twice to test idempotency
			clock.Step()
			easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/imagemanifest-001-before-upload-blob.sql")

			uploadBlob(t, h, token, "test1/foo", image.Config)
			uploadBlob(t, h, token, "test1/foo", image.Config)
			clock.Step()
			easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/imagemanifest-002-after-upload-blob.sql")

			uploadManifest(t, h, token, "test1/foo", image.Manifest, ref)
			uploadManifest(t, h, token, "test1/foo", image.Manifest, ref)
			clock.Step()
			if ref == "latest" {
				easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/imagemanifest-003-after-upload-manifest-by-tag.sql")
			} else {
				easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/imagemanifest-003-after-upload-manifest-by-digest.sql")
			}

			//since we did two PUTs, two events will have been logged
			auditEvents := []cadf.Event{{
				RequestPath: "/v2/test1/foo/manifests/" + ref,
				Action:      "create",
				Outcome:     "success",
				Reason:      test.CADFReasonOK,
				Target: cadf.Resource{
					TypeURI:   "docker-registry/account/repository/manifest",
					ID:        "test1/foo@" + image.Manifest.Digest.String(),
					ProjectID: "test1authtenant",
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
						ID:        "test1/foo:" + ref,
						ProjectID: "test1authtenant",
						Attachments: []cadf.Attachment{{
							Name:    "digest",
							TypeURI: "mime:text/plain",
							Content: image.Manifest.Digest.String(),
						}},
					},
				})
			}
			auditor.ExpectEvents(t, append(auditEvents, auditEvents...)...)

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
			if currentScenario.WithAnycast {
				testWithReplica(t, h, db, clock, "on_first_use", func(firstPass bool, h2 http.Handler, cfg2 keppel.Configuration, db2 *keppel.DB, ad2 *test.AuthDriver, sd2 *test.StorageDriver) {
					testAnycast(t, firstPass, db2, func() {
						anycastToken := getTokenForAnycast(t, h, ad, "repository:test1/foo:pull",
							keppel.CanPullFromAccount)
						anycastHeaders := map[string]string{
							"X-Forwarded-Host":  cfg.AnycastAPIPublicURL.Hostname(),
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
			_, err = db.Exec(
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

			//DELETE failure case: cannot delete by tag
			assert.HTTPRequest{
				Method:       "DELETE",
				Path:         "/v2/test1/foo/manifests/latest",
				Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
				ExpectStatus: http.StatusMethodNotAllowed,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrUnsupported),
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
			auditor.ExpectEvents(t /*, nothing */)

			//DELETE success case
			assert.HTTPRequest{
				Method:       "DELETE",
				Path:         "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
				Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
				ExpectStatus: http.StatusAccepted,
				ExpectHeader: test.VersionHeader,
			}.Check(t, h)
			clock.Step()
			easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/imagemanifest-004-after-delete-manifest.sql")

			//the DELETE will have logged an audit event
			auditor.ExpectEvents(t, cadf.Event{
				RequestPath: "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
				Action:      "delete",
				Outcome:     "success",
				Reason:      test.CADFReasonOK,
				Target: cadf.Resource{
					TypeURI:   "docker-registry/account/repository/manifest",
					ID:        "test1/foo@" + image.Manifest.Digest.String(),
					ProjectID: "test1authtenant",
				},
			})
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
	testWithPrimary(t, nil, func(h http.Handler, cfg keppel.Configuration, db *keppel.DB, ad *test.AuthDriver, sd *test.StorageDriver, fd *test.FederationDriver, clock *test.Clock, auditor *test.Auditor) {
		//This test builds on TestImageManifestLifecycle and provides test coverage
		//for the parts of the manifest push workflow that check manifest-manifest
		//references. (We don't have those in plain images, only in image lists.)
		token := getToken(t, h, ad, "repository:test1/foo:pull,push",
			keppel.CanPullFromAccount,
			keppel.CanPushToAccount)
		deleteToken := getToken(t, h, ad, "repository:test1/foo:delete",
			keppel.CanDeleteFromAccount)

		//as a setup, upload two images and render a third image that's not uploaded
		image1 := test.GenerateImage(test.GenerateExampleLayer(1))
		image2 := test.GenerateImage(test.GenerateExampleLayer(2))
		image3 := test.GenerateImage(test.GenerateExampleLayer(3))
		clock.Step()
		uploadBlob(t, h, token, "test1/foo", image1.Layers[0])
		uploadBlob(t, h, token, "test1/foo", image1.Config)
		uploadManifest(t, h, token, "test1/foo", image1.Manifest, "first")
		clock.Step()
		uploadBlob(t, h, token, "test1/foo", image2.Layers[0])
		uploadBlob(t, h, token, "test1/foo", image2.Config)
		uploadManifest(t, h, token, "test1/foo", image2.Manifest, "second")
		clock.Step()

		//PUT failure case: cannot upload image list manifest referencing missing manifests
		list1 := test.GenerateImageList(image1.Manifest, image3.Manifest)
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
		list2 := test.GenerateImageList(image1.Manifest, image2.Manifest)
		uploadManifest(t, h, token, "test1/foo", list2.Manifest, "list")

		clock.Step()
		easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/imagelistmanifest-001-after-upload-manifest.sql")

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
		clock.Step()
		easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/imagelistmanifest-002-after-delete-manifest.sql")
	})
}

func TestManifestQuotaExceeded(t *testing.T) {
	testWithPrimary(t, nil, func(h http.Handler, cfg keppel.Configuration, db *keppel.DB, ad *test.AuthDriver, sd *test.StorageDriver, fd *test.FederationDriver, clock *test.Clock, auditor *test.Auditor) {
		token := getToken(t, h, ad, "repository:test1/foo:pull,push",
			keppel.CanPullFromAccount,
			keppel.CanPushToAccount)

		//as a setup, upload two images
		image1 := test.GenerateImage(test.GenerateExampleLayer(1))
		image2 := test.GenerateImage(test.GenerateExampleLayer(2))
		uploadBlob(t, h, token, "test1/foo", image1.Layers[0])
		uploadBlob(t, h, token, "test1/foo", image1.Config)
		uploadManifest(t, h, token, "test1/foo", image1.Manifest, "first")
		uploadBlob(t, h, token, "test1/foo", image2.Layers[0])
		uploadBlob(t, h, token, "test1/foo", image2.Config)
		uploadManifest(t, h, token, "test1/foo", image2.Manifest, "second")

		//set quota below usage
		_, err := db.Exec(`UPDATE quotas SET manifests = $1`, 1)
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
	testWithPrimary(t, nil, func(h http.Handler, cfg keppel.Configuration, db *keppel.DB, ad *test.AuthDriver, sd *test.StorageDriver, fd *test.FederationDriver, clock *test.Clock, auditor *test.Auditor) {
		token := getToken(t, h, ad, "repository:test1/foo:pull,push",
			keppel.CanPullFromAccount,
			keppel.CanPushToAccount)

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
		uploadBlob(t, h, token, "test1/foo", blob)

		//setup required labels on account for failure
		_, err = db.Exec(
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
		_, err = db.Exec(
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
	})
}

func TestImageManifestWrongBlobSize(t *testing.T) {
	testWithPrimary(t, nil, func(h http.Handler, cfg keppel.Configuration, db *keppel.DB, ad *test.AuthDriver, sd *test.StorageDriver, fd *test.FederationDriver, clock *test.Clock, auditor *test.Auditor) {
		token := getToken(t, h, ad, "repository:test1/foo:pull,push",
			keppel.CanPullFromAccount,
			keppel.CanPushToAccount)

		//generate an image that references a layer, but the reference includes the wrong layer size
		layer := test.GenerateExampleLayer(1)
		uploadBlob(t, h, token, "test1/foo", layer)

		layer.Contents = append(layer.Contents, []byte("something")...)
		image := test.GenerateImage(layer)
		uploadBlob(t, h, token, "test1/foo", image.Config)

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
