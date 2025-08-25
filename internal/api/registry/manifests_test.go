// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/containers/image/v5/manifest"
	"github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/tasks"
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

			// on the API, we either reference the tag name (if uploading with tag) or the digest (if uploading without tag)
			ref := tagName
			if tagName == "" {
				ref = image.Manifest.Digest.String()
			}

			// repo does not exist before we first push to it
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

			// and even if it does...
			_, err := keppel.FindOrCreateRepository(s.DB, "foo", models.AccountName("test1"))
			test.MustDo(t, err)
			// ...the manifest does not exist before it is pushed
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

			// PUT failure case: cannot push with read-only token
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

			// PUT failure case: cannot push while account is being deleted
			testWithAccountIsDeleting(t, s.DB, "test1", func() {
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
						Message: "account is being deleted",
					},
				}.Check(t, h)
			})

			// PUT failure case: malformed manifest
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

			// PUT failure case: wrong digest
			assert.HTTPRequest{
				Method: "PUT",
				Path:   "/v2/test1/foo/manifests/" + test.DeterministicDummyDigest(1).String(),
				Header: map[string]string{
					"Authorization": "Bearer " + token,
					"Content-Type":  image.Manifest.MediaType,
				},
				Body:         assert.ByteData(image.Manifest.Contents),
				ExpectStatus: http.StatusBadRequest,
				ExpectBody:   test.ErrorCode(keppel.ErrDigestInvalid),
			}.Check(t, h)

			// PUT failure case: cannot upload manifest if referenced blob is missing
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

			// failed requests should not retain anything in the storage
			expectStorageEmpty(t, s.SD, s.DB)
			s.Auditor.ExpectEvents(t /*, nothing */)

			// PUT failure case: cannot upload manifest if referenced blob is uploaded, but in the wrong repo
			image.Config.MustUpload(t, s, barRepoRef)
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

			// PUT failure case: cannot upload manifest via the anycast API
			if currentlyWithAnycast {
				assert.HTTPRequest{
					Method: "PUT",
					Path:   "/v2/test1/foo/manifests/" + ref,
					Header: map[string]string{
						"Authorization":     "Bearer " + token,
						"Content-Type":      image.Manifest.MediaType,
						"X-Forwarded-Host":  s.Config.AnycastAPIPublicHostname,
						"X-Forwarded-Proto": "https",
					},
					Body:         assert.ByteData(image.Manifest.Contents),
					ExpectStatus: http.StatusMethodNotAllowed,
					ExpectHeader: test.VersionHeader,
					ExpectBody:   test.ErrorCode(keppel.ErrUnsupported),
				}.Check(t, h)
			}

			// PUT failure case: cannot upload manifest without Content-Type, or with
			// a faulty Content-Type (defense against attacks like CVE-2021-41190)
			for _, wrongMediaType := range []string{"", manifest.DockerV2ListMediaType} {
				assert.HTTPRequest{
					Method: "PUT",
					Path:   "/v2/test1/foo/manifests/" + ref,
					Header: map[string]string{
						"Authorization": "Bearer " + token,
						"Content-Type":  wrongMediaType,
					},
					Body:         assert.ByteData(image.Manifest.Contents),
					ExpectStatus: http.StatusBadRequest,
					ExpectBody:   test.ErrorCode(keppel.ErrManifestInvalid),
				}.Check(t, h)
			}

			// there should still not be any manifests
			s.Auditor.ExpectEvents(t /*, nothing */)

			// PUT success case: upload manifest (and also the blob referenced by it);
			// each PUT is executed twice to test idempotency
			s.Clock.StepBy(time.Second)
			easypg.AssertDBContent(t, s.DB.Db, "fixtures/imagemanifest-001-before-upload-blob.sql")

			image.Config.MustUpload(t, s, fooRepoRef)
			image.Config.MustUpload(t, s, fooRepoRef)
			s.Clock.StepBy(time.Second)
			easypg.AssertDBContent(t, s.DB.Db, "fixtures/imagemanifest-002-after-upload-blob.sql")

			// block overwrite should not trigger when pushing the same manifest twice
			test.MustExec(t, s.DB, `UPDATE accounts SET tag_policies_json = $2 WHERE name = $1`, "test1",
				test.ToJSON([]keppel.TagPolicy{{
					PolicyMatchRule: keppel.PolicyMatchRule{
						RepositoryRx: "foo",
					},
					BlockOverwrite: true,
				}}),
			)

			image.MustUpload(t, s, fooRepoRef, tagName)
			image.MustUpload(t, s, fooRepoRef, tagName)
			s.Clock.StepBy(time.Second)
			if ref == "latest" {
				easypg.AssertDBContent(t, s.DB.Db, "fixtures/imagemanifest-003-after-upload-manifest-by-tag.sql")
			} else {
				easypg.AssertDBContent(t, s.DB.Db, "fixtures/imagemanifest-003-after-upload-manifest-by-digest.sql")
			}

			if ref == "latest" {
				// block overwrite should prevent overwriting the tag
				assert.HTTPRequest{
					Method: "PUT",
					Path:   "/v2/test1/foo/manifests/" + ref,
					Header: map[string]string{
						"Authorization": "Bearer " + token,
						"Content-Type":  manifest.DockerV2Schema2MediaType,
					},
					Body:         assert.ByteData(test.GenerateImage(test.GenerateExampleLayer(1)).Manifest.Contents),
					ExpectStatus: http.StatusConflict,
					ExpectHeader: test.VersionHeader,
					ExpectBody: test.ErrorCodeWithMessage{
						Code:    keppel.ErrDenied,
						Message: "cannot overwrite tag \"latest\" as it is protected by a tag_policy",
					},
				}.Check(t, h)
			}

			// we did two PUTs, but only the first one will be logged since the second one did not change anything
			auditEvents := []cadf.Event{{
				RequestPath: "/v2/test1/foo/manifests/" + ref,
				Action:      cadf.CreateAction,
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
					Action:      cadf.CreateAction,
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

			// check GET/HEAD: manifest should now be available under the reference
			// where it was pushed to...
			expectManifestExists(t, h, readOnlyToken, "test1/foo", image.Manifest, ref, nil)
			// ...and under its digest
			expectManifestExists(t, h, readOnlyToken, "test1/foo", image.Manifest, image.Manifest.Digest.String(), nil)

			// GET failure case: wrong scope
			assert.HTTPRequest{
				Method:       "GET",
				Path:         "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
				Header:       map[string]string{"Authorization": "Bearer " + otherRepoToken},
				ExpectStatus: http.StatusUnauthorized,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrDenied),
			}.Check(t, h)
			// ^ NOTE: docker-registry sends UNAUTHORIZED (401) instead of DENIED (403)
			//        here, but 403 is more correct.

			// test GET via anycast
			if currentlyWithAnycast {
				testWithReplica(t, s, "on_first_use", func(firstPass bool, s2 test.Setup) {
					h2 := s2.Handler
					testAnycast(t, firstPass, s2.DB, func() {
						anycastToken := s.GetAnycastToken(t, "repository:test1/foo:pull")
						anycastHeaders := map[string]string{
							"X-Forwarded-Host":  s.Config.AnycastAPIPublicHostname,
							"X-Forwarded-Proto": "https",
						}
						expectManifestExists(t, h, anycastToken, "test1/foo", image.Manifest, ref, anycastHeaders)
						expectManifestExists(t, h, anycastToken, "test1/foo", image.Manifest, image.Manifest.Digest.String(), anycastHeaders)
						expectManifestExists(t, h2, anycastToken, "test1/foo", image.Manifest, ref, anycastHeaders)
						expectManifestExists(t, h2, anycastToken, "test1/foo", image.Manifest, image.Manifest.Digest.String(), anycastHeaders)
					})
				})
			}

			// test display of custom headers during GET/HEAD
			test.MustExec(t, s.DB,
				`UPDATE manifests SET min_layer_created_at = $1, max_layer_created_at = $2 WHERE digest = $3`,
				time.Unix(23, 0).UTC(), time.Unix(42, 0).UTC(), image.Manifest.Digest.String(),
			)
			test.MustExec(t, s.DB, `UPDATE trivy_security_info SET vuln_status = $1 WHERE digest = $2`, models.CleanSeverity, image.Manifest.Digest.String())

			for _, method := range []string{"GET", "HEAD"} {
				assert.HTTPRequest{
					Method:       method,
					Path:         "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
					Header:       map[string]string{"Authorization": "Bearer " + readOnlyToken},
					ExpectStatus: http.StatusOK,
					ExpectHeader: map[string]string{
						test.VersionHeaderKey:           test.VersionHeaderValue,
						"X-Keppel-Vulnerability-Status": string(models.CleanSeverity),
						"X-Keppel-Min-Layer-Created-At": "23",
						"X-Keppel-Max-Layer-Created-At": "42",
					},
				}.Check(t, h)
			}

			// test GET with anonymous user (fails unless a pull_anonymous RBAC policy is set up)
			assert.HTTPRequest{
				Method:       "GET",
				Path:         "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
				Header:       test.AddHeadersForCorrectAuthChallenge(nil),
				ExpectStatus: http.StatusUnauthorized,
				ExpectHeader: map[string]string{
					test.VersionHeaderKey: test.VersionHeaderValue,
					"Www-Authenticate":    `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="repository:test1/foo:pull"`,
				},
			}.Check(t, h)
			test.MustExec(t, s.DB, `UPDATE accounts SET rbac_policies_json = $2 WHERE name = $1`, "test1",
				test.ToJSON([]keppel.RBACPolicy{{
					RepositoryPattern: "foo",
					Permissions:       []keppel.RBACPermission{keppel.RBACAnonymousPullPermission},
				}}),
			)
			assert.HTTPRequest{
				Method:       "GET",
				Path:         "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
				ExpectStatus: http.StatusOK,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   assert.ByteData(image.Manifest.Contents),
			}.Check(t, h)
			test.MustExec(t, s.DB, `UPDATE accounts SET rbac_policies_json = $2 WHERE name = $1`, "test1", "")

			// DELETE failure case: no delete permission
			assert.HTTPRequest{
				Method:       "DELETE",
				Path:         "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
				Header:       map[string]string{"Authorization": "Bearer " + token},
				ExpectStatus: http.StatusUnauthorized,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrDenied),
			}.Check(t, h)

			// DELETE failure case: unknown manifest
			assert.HTTPRequest{
				Method:       "DELETE",
				Path:         "/v2/test1/foo/manifests/" + test.DeterministicDummyDigest(1).String(),
				Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
				ExpectStatus: http.StatusNotFound,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrManifestUnknown),
			}.Check(t, h)

			// DELETE failure case: cannot delete blob while the manifest still exists in the DB
			assert.HTTPRequest{
				Method:       "DELETE",
				Path:         "/v2/test1/foo/blobs/" + image.Config.Digest.String(),
				Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
				ExpectStatus: http.StatusMethodNotAllowed,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrUnsupported),
			}.Check(t, h)

			test.MustExec(t, s.DB, `UPDATE accounts SET tag_policies_json = $2 WHERE name = $1`, "test1",
				test.ToJSON([]keppel.TagPolicy{{
					PolicyMatchRule: keppel.PolicyMatchRule{
						RepositoryRx: "foo",
					},
					BlockDelete: true,
				}}),
			)

			// DELETE failure case: tag is protected by tag policy
			assert.HTTPRequest{
				Method:       "DELETE",
				Path:         "/v2/test1/foo/manifests/" + ref,
				Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
				ExpectStatus: http.StatusConflict,
				ExpectHeader: test.VersionHeader,
			}.Check(t, h)

			test.MustExec(t, s.DB, `UPDATE accounts SET tag_policies_json = '[]' WHERE name = $1`, "test1")

			// no deletes were successful yet, so...
			s.Auditor.ExpectEvents(t /*, nothing */)

			// DELETE success case
			assert.HTTPRequest{
				Method:       "DELETE",
				Path:         "/v2/test1/foo/manifests/" + ref,
				Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
				ExpectStatus: http.StatusAccepted,
				ExpectHeader: test.VersionHeader,
			}.Check(t, h)
			s.Clock.StepBy(time.Second)
			if ref == "latest" {
				easypg.AssertDBContent(t, s.DB.Db, "fixtures/imagemanifest-004-after-delete-tag.sql")
			} else {
				easypg.AssertDBContent(t, s.DB.Db, "fixtures/imagemanifest-004-after-delete-manifest.sql")
			}

			// the DELETE will have logged an audit event
			event := cadf.Event{
				RequestPath: "/v2/test1/foo/manifests/" + ref,
				Action:      cadf.DeleteAction,
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
		// This test builds on TestImageManifestLifecycle and provides test coverage
		// for the parts of the manifest push workflow that check manifest-manifest
		// references. (We don't have those in plain images, only in image lists.)
		h := s.Handler
		token := s.GetToken(t, "repository:test1/foo:pull,push")
		deleteToken := s.GetToken(t, "repository:test1/foo:delete")

		// as a setup, upload two images and render a third image that's not uploaded
		image1 := test.GenerateImage(test.GenerateExampleLayer(1))
		image2 := test.GenerateImage(test.GenerateExampleLayer(2))
		image3 := test.GenerateImage(test.GenerateExampleLayer(3))
		s.Clock.StepBy(time.Second)
		image1.MustUpload(t, s, fooRepoRef, "first")
		s.Clock.StepBy(time.Second)
		image2.MustUpload(t, s, fooRepoRef, "second")
		s.Clock.StepBy(time.Second)

		// PUT failure case: cannot upload image list manifest referencing missing manifests
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

		// PUT success case: upload image list manifest referencing available manifests
		list2 := test.GenerateImageList(image1, image2)
		list2.MustUpload(t, s, fooRepoRef, "list")

		s.Clock.StepBy(time.Second)
		easypg.AssertDBContent(t, s.DB.Db, "fixtures/imagelistmanifest-001-after-upload-manifest.sql")

		// check GET for manifest list
		expectManifestExists(t, h, token, "test1/foo", list2.Manifest, "list", nil)

		// as a special case, GET on the manifest list returns the linux/amd64
		// manifest if only single-arch manifests are accepted by the client (this
		// behavior is somewhat dubious, but required for full compatibility with
		// existing clients)
		assert.HTTPRequest{
			Method: "GET",
			Path:   "/v2/test1/foo/manifests/" + list2.Manifest.Digest.String(),
			Header: map[string]string{
				"Authorization": "Bearer " + token,
				"Accept":        manifest.DockerV2Schema2MediaType,
			},
			ExpectStatus: http.StatusTemporaryRedirect,
			ExpectHeader: map[string]string{
				test.VersionHeaderKey: test.VersionHeaderValue,
				"Location":            "/v2/test1/foo/manifests/" + image1.Manifest.Digest.String(),
			},
		}.Check(t, h)
		// but we return the whole list if at all possible
		expectManifestExists(t, h, token, "test1/foo", list2.Manifest, "list", map[string]string{
			"Accept": "application/vnd.docker.distribution.manifest.v2+json, application/vnd.docker.distribution.manifest.list.v2+json",
		})

		// DELETE failure case: cannot delete manifest list while the manifest still exists in the DB
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/v2/test1/foo/manifests/" + image1.Manifest.Digest.String(),
			Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
			ExpectStatus: http.StatusConflict,
			ExpectHeader: test.VersionHeader,
			ExpectBody: test.ErrorCodeWithMessage{
				Code:    keppel.ErrDenied,
				Message: "cannot delete a manifest which is referenced by the manifest " + list2.Manifest.Digest.String(),
			},
		}.Check(t, h)

		// DELETE success case
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/v2/test1/foo/manifests/" + list2.Manifest.Digest.String(),
			Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
			ExpectStatus: http.StatusAccepted,
			ExpectHeader: test.VersionHeader,
		}.Check(t, h)
		s.Clock.StepBy(time.Second)
		easypg.AssertDBContent(t, s.DB.Db, "fixtures/imagelistmanifest-002-after-delete-manifest.sql")
	})
}

func TestManifestQuotaExceeded(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler
		token := s.GetToken(t, "repository:test1/foo:pull,push")

		// as a setup, upload two images
		image1 := test.GenerateImage(test.GenerateExampleLayer(1))
		image2 := test.GenerateImage(test.GenerateExampleLayer(2))
		image1.MustUpload(t, s, fooRepoRef, "first")
		image2.MustUpload(t, s, fooRepoRef, "second")

		// set quota below usage
		test.MustExec(t, s.DB, `UPDATE quotas SET manifests = $1`, 1)

		quotaExceededMessage := test.ErrorCodeWithMessage{
			Code:    keppel.ErrDenied,
			Message: "manifest quota exceeded (quota = 1, usage = 2)",
		}

		// further blob uploads are not possible now
		assert.HTTPRequest{
			Method:       "POST",
			Path:         "/v2/test1/foo/blobs/uploads/",
			Header:       map[string]string{"Authorization": "Bearer " + token},
			ExpectStatus: http.StatusConflict,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   quotaExceededMessage,
		}.Check(t, h)

		// further manifest uploads are not possible now
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

func TestRuleForManifest(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler
		token := s.GetToken(t, "repository:test1/foo:pull,push")

		image := test.GenerateImageWithCustomConfig(func(cfg map[string]any) {
			cfg["config"].(map[string]any)["Labels"] = map[string]string{"foo": "is there", "bar": "is there"}
		}, test.GenerateExampleLayer(1))
		image.Config.MustUpload(t, s, fooRepoRef)
		image.Layers[0].MustUpload(t, s, fooRepoRef)

		// setup rule for manifest on account for failure
		test.MustExec(t, s.DB,
			`UPDATE accounts SET rule_for_manifest = $1 WHERE name = $2`,
			"'random-label-that-does-not-exist' in labels", "test1",
		)

		// manifest push should fail
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/v2/test1/foo/manifests/latest",
			Header: map[string]string{
				"Authorization": "Bearer " + token,
				"Content-Type":  manifest.DockerV2Schema2MediaType,
			},
			Body:         assert.ByteData(image.Manifest.Contents),
			ExpectStatus: http.StatusBadRequest,
			ExpectHeader: test.VersionHeader,
			ExpectBody: test.ErrorCodeWithMessage{
				Code:    keppel.ErrManifestInvalid,
				Message: "rule is not satisfied: 'random-label-that-does-not-exist' in labels",
			},
		}.Check(t, h)

		// setup required labels on account for success
		test.MustExec(t, s.DB,
			`UPDATE accounts SET rule_for_manifest = $1 WHERE name = $2`,
			"'foo' in labels && 'bar' in labels", "test1",
		)

		// manifest push should succeed
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/v2/test1/foo/manifests/latest",
			Header: map[string]string{
				"Authorization": "Bearer " + token,
				"Content-Type":  manifest.DockerV2Schema2MediaType,
			},
			Body:         assert.ByteData(image.Manifest.Contents),
			ExpectStatus: http.StatusCreated,
			ExpectHeader: test.VersionHeader,
		}.Check(t, h)

		// check that the labels_json field is populated correctly in the DB
		expectLabelsJSONOnManifest(
			t, s.DB, image.Manifest.Digest,
			map[string]string{"bar": "is there", "foo": "is there"},
		)

		// upload another image with similar (but not identical) labels as
		// preparation for the image list test below
		otherImage := test.GenerateImageWithCustomConfig(func(cfg map[string]any) {
			cfg["config"].(map[string]any)["Labels"] = map[string]string{"foo": "is there", "bar": "is different"}
		}, image.Layers[0])
		otherImage.MustUpload(t, s, fooRepoRef, "other")

		// rule_for_manifest does not apply to image lists (since image list manifests
		// do not have labels at all), so we can upload this list manifest without
		// any additional considerations
		list := test.GenerateImageList(image, otherImage)
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/v2/test1/foo/manifests/list",
			Header: map[string]string{
				"Authorization": "Bearer " + token,
				"Content-Type":  manifest.DockerV2ListMediaType,
			},
			Body:         assert.ByteData(list.Manifest.Contents),
			ExpectStatus: http.StatusCreated,
			ExpectHeader: test.VersionHeader,
		}.Check(t, h)

		// check the labels_json field on the list manifest
		expectLabelsJSONOnManifest(
			t, s.DB, list.Manifest.Digest,
			map[string]string{"foo": "is there"}, // the "bar" label differs between `image` and `otherImage`
		)
	})
}

func expectLabelsJSONOnManifest(t *testing.T, db *keppel.DB, manifestDigest digest.Digest, expected map[string]string) {
	t.Helper()
	labelsJSONStr, err := db.SelectStr(`SELECT labels_json FROM manifests WHERE digest = $1`, manifestDigest.String())
	test.MustDo(t, err)

	var actual map[string]string
	test.MustDo(t, json.Unmarshal([]byte(labelsJSONStr), &actual))
	assert.DeepEqual(t, "labels_json", actual, expected)
}

func TestImageManifestWrongBlobSize(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler
		token := s.GetToken(t, "repository:test1/foo:pull,push")

		// generate an image that references a layer, but the reference includes the wrong layer size
		layer := test.GenerateExampleLayer(1)
		layer.MustUpload(t, s, fooRepoRef)

		layer.Contents = append(layer.Contents, []byte("something")...)
		image := test.GenerateImage(layer)
		image.Config.MustUpload(t, s, fooRepoRef)

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

func TestImageManifestCmdEntrypointAsString(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		j := tasks.NewJanitor(s.Config, s.FD, s.SD, s.ICD, s.DB, s.AMD, s.Auditor).OverrideTimeNow(s.Clock.Now).OverrideGenerateStorageID(s.SIDGenerator.Next)
		j.DisableJitter()
		validateManifestJob := j.ManifestValidationJob(s.Registry)

		// generate an image that has strings as Entrypoint and Cmd
		image := test.GenerateImageWithCustomConfig(func(cfg map[string]any) {
			cfg["config"].(map[string]any)["Cmd"] = "/usr/bin/env bash"
		}, test.GenerateExampleLayer(1))
		image.MustUpload(t, s, fooRepoRef, "first")

		s.Clock.StepBy(36 * time.Hour)
		err := validateManifestJob.ProcessOne(s.Ctx)
		if err != nil {
			t.Error("expected err = nil, but got: " + err.Error())
		}
	})
}

func TestManifestAnnotations(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler
		token := s.GetToken(t, "repository:test1/foo:pull,push")

		image := test.GenerateOCIImage(test.OCIArgs{
			ConfigMediaType: imgspecv1.MediaTypeImageManifest,
			Annotations: map[string]string{
				"abc": "def",
			}},
		)
		image.MustUpload(t, s, fooRepoRef, "latest")

		// manifest push should succeed
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/v2/test1/foo/manifests/latest",
			Header: map[string]string{
				"Authorization": "Bearer " + token,
				"Content-Type":  imgspecv1.MediaTypeImageManifest,
			},
			Body:         assert.ByteData(image.Manifest.Contents),
			ExpectStatus: http.StatusCreated,
			ExpectHeader: test.VersionHeader,
		}.Check(t, h)

		// check that the annotations_json field is populated correctly in the DB
		labelsJSONStr, err := s.DB.SelectStr(`SELECT annotations_json FROM manifests WHERE digest = $1`, image.Manifest.Digest.String())
		test.MustDo(t, err)

		var actual map[string]string
		must.Succeed(json.Unmarshal([]byte(labelsJSONStr), &actual))
		assert.DeepEqual(t, "annotations_json", actual, map[string]string{"abc": "def"})
	})
}

func TestManifestArtifactType(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler
		token := s.GetToken(t, "repository:test1/foo:pull,push")

		artifactType := "application/vnd.oci.artifact.config.v1+json"
		image := test.GenerateOCIImage(test.OCIArgs{ArtifactType: artifactType})
		image.MustUpload(t, s, fooRepoRef, "latest")

		// manifest push should succeed
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/v2/test1/foo/manifests/latest",
			Header: map[string]string{
				"Authorization": "Bearer " + token,
				"Content-Type":  imgspecv1.MediaTypeImageManifest,
			},
			Body:         assert.ByteData(image.Manifest.Contents),
			ExpectStatus: http.StatusCreated,
			ExpectHeader: test.VersionHeader,
		}.Check(t, h)

		// check that the annotations_json field is populated correctly in the DB
		artifactTypeStr, err := s.DB.SelectStr(`SELECT artifact_type FROM manifests WHERE digest = $1`, image.Manifest.Digest.String())
		test.MustDo(t, err)

		assert.DeepEqual(t, "artifact_type", artifactType, artifactTypeStr)
	})
}
