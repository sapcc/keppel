// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/httptest"
	"github.com/sapcc/go-bits/must"
	"go.podman.io/image/v5/manifest"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/tasks"
	"github.com/sapcc/keppel/internal/test"
)

func TestImageManifestLifecycle(t *testing.T) {
	ctx := t.Context()
	image := test.GenerateImage( /* no layers */ )

	for _, tagName := range []string{"latest", ""} {
		testWithPrimary(t, nil, func(s test.Setup) {
			// set up tag policies to test block_overwrite and block_push
			// (setting this up early also proves that unrelated pushes, e.g. of manifests without tag, are not inhibited)
			test.MustExec(t, s.DB, `UPDATE accounts SET tag_policies_json = $2 WHERE name = $1`, "test1",
				test.ToJSON([]keppel.TagPolicy{
					{
						PolicyMatchRule: keppel.PolicyMatchRule{
							RepositoryRx: "foo",
						},
						BlockOverwrite: true,
					},
					{
						PolicyMatchRule: keppel.PolicyMatchRule{
							RepositoryRx: "foo",
							TagRx:        "dangerous.*",
						},
						BlockPush: true,
					},
				}),
			)

			tokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull,push")
			readOnlyTokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull")
			otherRepoTokenHeaders := s.GetTokenHeaders(t, "repository:test1/bar:pull,push")
			deleteTokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:delete")

			// on the API, we either reference the tag name (if uploading with tag) or the digest (if uploading without tag)
			ref := cmp.Or(tagName, image.Manifest.Digest.String())

			// repo does not exist before we first push to it
			for _, method := range []string{"GET", "HEAD"} {
				resp := s.RespondTo(ctx, fmt.Sprintf("%s /v2/test1/foo/manifests/%s", method, ref),
					httptest.WithHeaders(readOnlyTokenHeaders),
				)
				if method == "GET" {
					resp.ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrNameUnknown))
				} else {
					resp.ExpectStatus(t, http.StatusNotFound)
				}
			}

			// and even if it does...
			_ = must.ReturnT(keppel.FindOrCreateRepository(s.DB, "foo", models.AccountName("test1")))(t)
			// ...the manifest does not exist before it is pushed
			for _, method := range []string{"GET", "HEAD"} {
				resp := s.RespondTo(ctx, fmt.Sprintf("%s /v2/test1/foo/manifests/%s", method, ref),
					httptest.WithHeaders(readOnlyTokenHeaders),
				)
				if method == "GET" {
					resp.ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrManifestUnknown))
				} else {
					resp.ExpectStatus(t, http.StatusNotFound)
				}
			}

			// PUT failure case: cannot push with read-only token
			s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/"+ref,
				httptest.WithHeaders(readOnlyTokenHeaders),
				uploadingManifest(image.Manifest),
			).ExpectJSON(t, http.StatusUnauthorized, test.ErrorCode(keppel.ErrDenied))

			// PUT failure case: cannot push while account is being deleted
			testWithAccountIsDeleting(t, s.DB, "test1", func() {
				s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/"+ref,
					httptest.WithHeaders(tokenHeaders),
					uploadingManifest(image.Manifest),
				).ExpectJSON(t, http.StatusMethodNotAllowed, test.ErrorCodeWithMessage{
					Code:    keppel.ErrUnsupported,
					Message: "account is being deleted",
				})
			})

			for _, repo := range []string{"_blobs", "_chunks", `-invalid`} {
				// PUT failure case: invalid repository names (e.g. repos starting with '_' or '-'),
				// including reserved internal repos _blobs and _chunks and the '-invalid' case
				s.RespondTo(ctx, fmt.Sprintf("PUT /v2/test1/%s/manifests/%s", repo, ref),
					httptest.WithHeaders(tokenHeaders),
					uploadingManifest(image.Manifest),
				).ExpectJSON(t, http.StatusBadRequest, test.ErrorCode(keppel.ErrNameInvalid))
			}

			// PUT failure case: malformed manifest
			malformedManifest := test.Bytes{
				MediaType: image.Manifest.MediaType,
				Contents:  append([]byte("wtf"), image.Manifest.Contents...),
			}
			s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/"+ref,
				httptest.WithHeaders(tokenHeaders),
				uploadingManifest(malformedManifest),
			).ExpectJSON(t, http.StatusBadRequest, test.ErrorCode(keppel.ErrManifestInvalid))

			// PUT failure case: wrong digest
			s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/"+test.DeterministicDummyDigest(1).String(),
				httptest.WithHeaders(tokenHeaders),
				uploadingManifest(image.Manifest),
			).ExpectJSON(t, http.StatusBadRequest, test.ErrorCode(keppel.ErrDigestInvalid))

			// PUT failure case: cannot upload manifest if referenced blob is missing
			s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/"+ref,
				httptest.WithHeaders(tokenHeaders),
				uploadingManifest(image.Manifest),
			).ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrManifestBlobUnknown))

			// failed requests should not retain anything in the storage
			expectStorageEmpty(t, s.SD, s.DB)
			s.Auditor.ExpectEvents(t /*, nothing */)

			// PUT failure case: cannot upload manifest if referenced blob is uploaded, but in the wrong repo
			image.Config.MustUpload(t, s, barRepoRef)
			s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/"+ref,
				httptest.WithHeaders(tokenHeaders),
				uploadingManifest(image.Manifest),
			).ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrManifestBlobUnknown))

			// PUT failure case: cannot upload manifest via the anycast API
			if currentlyWithAnycast {
				s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/"+ref,
					httptest.WithHeaders(tokenHeaders),
					httptest.WithHeaders(http.Header{
						"X-Forwarded-Host":  {s.Config.AnycastAPIPublicHostname},
						"X-Forwarded-Proto": {"https"},
					}),
					uploadingManifest(image.Manifest),
				).ExpectJSON(t, http.StatusMethodNotAllowed, test.ErrorCode(keppel.ErrUnsupported))
			}

			// PUT failure case: cannot upload manifest without Content-Type, or with
			// a faulty Content-Type (defense against attacks like CVE-2021-41190)
			for _, wrongMediaType := range []string{"", manifest.DockerV2ListMediaType} {
				s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/"+ref,
					httptest.WithHeaders(tokenHeaders),
					uploadingManifest(image.Manifest),
					httptest.WithHeader("Content-Type", wrongMediaType), // overrides Content-Type within uploadingManifest()
				).ExpectJSON(t, http.StatusBadRequest, test.ErrorCode(keppel.ErrManifestInvalid))
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

			// block_overwrite should not trigger when pushing the same manifest twice
			image.MustUpload(t, s, fooRepoRef, tagName)
			image.MustUpload(t, s, fooRepoRef, tagName)
			s.Clock.StepBy(time.Second)
			if ref == "latest" {
				easypg.AssertDBContent(t, s.DB.Db, "fixtures/imagemanifest-003-after-upload-manifest-by-tag.sql")
			} else {
				easypg.AssertDBContent(t, s.DB.Db, "fixtures/imagemanifest-003-after-upload-manifest-by-digest.sql")
			}

			if ref == "latest" {
				// block_overwrite should prevent overwriting the tag
				updatedImage := test.GenerateImage(test.GenerateExampleLayer(1))
				s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/"+ref,
					httptest.WithHeaders(tokenHeaders),
					uploadingManifest(updatedImage.Manifest),
				).ExpectJSON(t, http.StatusConflict, test.ErrorCodeWithMessage{
					Code:    keppel.ErrDenied,
					Message: "cannot overwrite tag \"latest\" as it is protected by a tag_policy",
				})
			}

			// block_push should prevent matching tags from being pushed at all
			s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/dangerous-release",
				httptest.WithHeaders(tokenHeaders),
				uploadingManifest(image.Manifest),
			).ExpectJSON(t, http.StatusConflict, test.ErrorCodeWithMessage{
				Code:    keppel.ErrDenied,
				Message: "cannot push tag \"dangerous-release\" as it is forbidden by a tag_policy",
			})

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
			expectManifestExists(t, s, readOnlyTokenHeaders, "test1/foo", image.Manifest, ref)
			// ...and under its digest
			expectManifestExists(t, s, readOnlyTokenHeaders, "test1/foo", image.Manifest, image.Manifest.Digest.String())

			// GET failure case: wrong scope
			s.RespondTo(ctx, "GET /v2/test1/foo/manifests/"+image.Manifest.Digest.String(),
				httptest.WithHeaders(otherRepoTokenHeaders),
			).ExpectJSON(t, http.StatusUnauthorized, test.ErrorCode(keppel.ErrDenied))
			// ^ NOTE: docker-registry sends UNAUTHORIZED (401) instead of DENIED (403)
			//        here, but 403 is more correct.

			// test GET via anycast
			if currentlyWithAnycast {
				testWithReplica(t, s, "on_first_use", func(firstPass bool, s2 test.Setup) {
					testAnycast(t, firstPass, s2.DB, func() {
						anycastTokenHeaders := s.GetAnycastTokenHeaders(t, "repository:test1/foo:pull")
						expectManifestExists(t, s, anycastTokenHeaders, "test1/foo", image.Manifest, ref)
						expectManifestExists(t, s, anycastTokenHeaders, "test1/foo", image.Manifest, image.Manifest.Digest.String())
						expectManifestExists(t, s2, anycastTokenHeaders, "test1/foo", image.Manifest, ref)
						expectManifestExists(t, s2, anycastTokenHeaders, "test1/foo", image.Manifest, image.Manifest.Digest.String())
					})
				})
			}

			// test display of custom headers during GET/HEAD
			test.MustExec(t, s.DB,
				`UPDATE manifests SET min_layer_created_at = $1, max_layer_created_at = $2 WHERE digest = $3`,
				time.Unix(23, 0).UTC(), time.Unix(42, 0).UTC(), image.Manifest.Digest.String(),
			)
			test.MustExec(t, s.DB,
				`UPDATE trivy_security_info SET vuln_status = $1 WHERE digest = $2`,
				models.CleanSeverity, image.Manifest.Digest.String(),
			)

			for _, method := range []string{"GET", "HEAD"} {
				s.RespondTo(ctx, method+" /v2/test1/foo/manifests/"+image.Manifest.Digest.String(),
					httptest.WithHeaders(readOnlyTokenHeaders),
				).ExpectHeaders(t, http.Header{
					"X-Keppel-Vulnerability-Status": {string(models.CleanSeverity)},
					"X-Keppel-Min-Layer-Created-At": {"23"},
					"X-Keppel-Max-Layer-Created-At": {"42"},
				}).ExpectStatus(t, http.StatusOK)
			}

			// test GET with anonymous user (fails unless a pull_anonymous RBAC policy is set up)
			s.RespondTo(ctx, "GET /v2/test1/foo/manifests/"+image.Manifest.Digest.String()).
				ExpectHeader(t, "Www-Authenticate", `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="repository:test1/foo:pull"`).
				ExpectStatus(t, http.StatusUnauthorized)

			test.MustExec(t, s.DB, `UPDATE accounts SET rbac_policies_json = $2 WHERE name = $1`, "test1",
				test.ToJSON([]keppel.RBACPolicy{{
					RepositoryPattern: "foo",
					Permissions:       []keppel.RBACPermission{keppel.RBACAnonymousPullPermission},
				}}),
			)

			s.RespondTo(ctx, "GET /v2/test1/foo/manifests/"+image.Manifest.Digest.String()).
				Expect(containsManifest(t, image.Manifest))

			test.MustExec(t, s.DB, `UPDATE accounts SET rbac_policies_json = $2 WHERE name = $1`, "test1", "")

			// DELETE failure case: no delete permission
			s.RespondTo(ctx, "DELETE /v2/test1/foo/manifests/"+image.Manifest.Digest.String(),
				httptest.WithHeaders(tokenHeaders),
			).ExpectJSON(t, http.StatusUnauthorized, test.ErrorCode(keppel.ErrDenied))

			// DELETE failure case: unknown manifest
			s.RespondTo(ctx, "DELETE /v2/test1/foo/manifests/"+test.DeterministicDummyDigest(1).String(),
				httptest.WithHeaders(deleteTokenHeaders),
			).ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrManifestUnknown))

			// DELETE failure case: cannot delete blob while the manifest still exists in the DB
			s.RespondTo(ctx, "DELETE /v2/test1/foo/blobs/"+image.Config.Digest.String(),
				httptest.WithHeaders(deleteTokenHeaders),
			).ExpectJSON(t, http.StatusMethodNotAllowed, test.ErrorCode(keppel.ErrUnsupported))

			// DELETE failure case: tag is protected by tag policy
			test.MustExec(t, s.DB, `UPDATE accounts SET tag_policies_json = $2 WHERE name = $1`, "test1",
				test.ToJSON([]keppel.TagPolicy{{
					PolicyMatchRule: keppel.PolicyMatchRule{
						RepositoryRx: "foo",
					},
					BlockDelete: true,
				}}),
			)

			s.RespondTo(ctx, "DELETE /v2/test1/foo/manifests/"+ref,
				httptest.WithHeaders(deleteTokenHeaders),
			).ExpectJSON(t, http.StatusConflict, test.ErrorCode(keppel.ErrDenied))

			test.MustExec(t, s.DB, `UPDATE accounts SET tag_policies_json = '[]' WHERE name = $1`, "test1")

			// no deletes were successful yet, so...
			s.Auditor.ExpectEvents(t /*, nothing */)

			// DELETE success case
			s.RespondTo(ctx, "DELETE /v2/test1/foo/manifests/"+ref,
				httptest.WithHeaders(deleteTokenHeaders),
			).ExpectStatus(t, http.StatusAccepted)
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

func TestImageListManifestLifecycle(t *testing.T) {
	ctx := t.Context()

	testWithPrimary(t, nil, func(s test.Setup) {
		// This test builds on TestImageManifestLifecycle and provides test coverage
		// for the parts of the manifest push workflow that check manifest-manifest
		// references. (We don't have those in plain images, only in image lists.)
		tokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull,push")
		deleteTokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:delete")

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
		s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/"+list1.Manifest.Digest.String(),
			httptest.WithHeaders(tokenHeaders),
			uploadingManifest(list1.Manifest),
		).ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrManifestUnknown))

		// PUT success case: upload image list manifest referencing available manifests
		list2 := test.GenerateImageList(image1, image2)
		list2.MustUpload(t, s, fooRepoRef, "list")

		s.Clock.StepBy(time.Second)
		easypg.AssertDBContent(t, s.DB.Db, "fixtures/imagelistmanifest-001-after-upload-manifest.sql")

		// check GET for manifest list
		expectManifestExists(t, s, tokenHeaders, "test1/foo", list2.Manifest, "list")

		// as a special case, GET on the manifest list returns the linux/amd64
		// manifest if only single-arch manifests are accepted by the client (this
		// behavior is somewhat dubious, but required for full compatibility with
		// existing clients)
		s.RespondTo(ctx, "GET /v2/test1/foo/manifests/"+list2.Manifest.Digest.String(),
			httptest.WithHeaders(tokenHeaders),
			httptest.WithHeader("Accept", manifest.DockerV2Schema2MediaType),
		).
			ExpectHeader(t, "Location", "/v2/test1/foo/manifests/"+image1.Manifest.Digest.String()).
			ExpectStatus(t, http.StatusTemporaryRedirect)

		// but we return the whole list if at all possible
		tokenHeaders.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json, application/vnd.docker.distribution.manifest.list.v2+json")
		expectManifestExists(t, s, tokenHeaders, "test1/foo", list2.Manifest, "list")

		// DELETE failure case: cannot delete manifest list while the manifest still exists in the DB
		s.RespondTo(ctx, "DELETE /v2/test1/foo/manifests/"+image1.Manifest.Digest.String(),
			httptest.WithHeaders(deleteTokenHeaders),
		).ExpectJSON(t, http.StatusConflict, test.ErrorCodeWithMessage{
			Code:    keppel.ErrDenied,
			Message: "cannot delete a manifest which is referenced by the manifest " + list2.Manifest.Digest.String(),
		})

		// DELETE success case
		s.RespondTo(ctx, "DELETE /v2/test1/foo/manifests/"+list2.Manifest.Digest.String(),
			httptest.WithHeaders(deleteTokenHeaders),
		).ExpectStatus(t, http.StatusAccepted)
		s.Clock.StepBy(time.Second)
		easypg.AssertDBContent(t, s.DB.Db, "fixtures/imagelistmanifest-002-after-delete-manifest.sql")
	})
}

func TestManifestQuotaExceeded(t *testing.T) {
	ctx := t.Context()

	testWithPrimary(t, nil, func(s test.Setup) {
		tokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull,push")

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
		s.RespondTo(ctx, "POST /v2/test1/foo/blobs/uploads/", httptest.WithHeaders(tokenHeaders)).
			ExpectJSON(t, http.StatusConflict, quotaExceededMessage)

		// further manifest uploads are not possible now
		s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/anotherone", httptest.WithHeaders(tokenHeaders)).
			ExpectJSON(t, http.StatusConflict, quotaExceededMessage)
	})
}

func TestRuleForManifest(t *testing.T) {
	ctx := t.Context()

	testWithPrimary(t, nil, func(s test.Setup) {
		tokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull,push")

		labels := map[string]string{"foo": "is there", "bar": "is there"}

		image := test.GenerateImageWithCustomConfig(func(cfg map[string]any) {
			cfg["config"].(map[string]any)["Labels"] = labels
		}, test.GenerateExampleLayer(1))
		image.Config.MustUpload(t, s, fooRepoRef)
		image.Layers[0].MustUpload(t, s, fooRepoRef)

		imageOCI := test.GenerateOCIImage(test.OCIArgs{
			Config: map[string]any{
				"config": map[string]any{
					"Labels": labels,
				},
			},
		}, image.Layers...)
		imageOCI.Config.MustUpload(t, s, fooRepoRef)
		imageOCI.Layers[0].MustUpload(t, s, fooRepoRef)

		// setup rule for manifest on account for failure
		test.MustExec(t, s.DB,
			`UPDATE accounts SET rule_for_manifest = $1 WHERE name = $2`,
			"'random-label-that-does-not-exist' in labels", "test1",
		)

		// docker manifest push should fail ...
		s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/latest",
			httptest.WithHeaders(tokenHeaders),
			uploadingManifest(image.Manifest),
		).ExpectJSON(t, http.StatusBadRequest, test.ErrorCodeWithMessage{
			Code:    keppel.ErrManifestInvalid,
			Message: "manifest upload {\"labels\":{\"bar\":\"is there\",\"foo\":\"is there\"},\"layers\":[{\"annotations\":null,\"media_type\":\"application/vnd.docker.image.rootfs.diff.tar.gzip\"}],\"media_type\":\"application/vnd.docker.distribution.manifest.v2+json\",\"repo_name\":\"foo\"} does not satisfy validation rule: 'random-label-that-does-not-exist' in labels",
		})

		// ... and OCI, too
		s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/latest",
			httptest.WithHeaders(tokenHeaders),
			uploadingManifest(imageOCI.Manifest),
		).ExpectJSON(t, http.StatusBadRequest, test.ErrorCodeWithMessage{
			Code:    keppel.ErrManifestInvalid,
			Message: "manifest upload {\"labels\":{\"bar\":\"is there\",\"foo\":\"is there\"},\"layers\":[{\"annotations\":null,\"media_type\":\"application/vnd.docker.image.rootfs.diff.tar.gzip\"}],\"media_type\":\"application/vnd.oci.image.manifest.v1+json\",\"repo_name\":\"foo\"} does not satisfy validation rule: 'random-label-that-does-not-exist' in labels",
		})

		// setup required labels on account for success
		test.MustExec(t, s.DB,
			`UPDATE accounts SET rule_for_manifest = $1 WHERE name = $2`,
			"'foo' in labels && 'bar' in labels", "test1",
		)

		// docker manifest push should succeed when all labels are there ...
		s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/latest",
			httptest.WithHeaders(tokenHeaders),
			uploadingManifest(image.Manifest),
		).ExpectStatus(t, http.StatusCreated)

		// ... and OCI, too
		s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/latest",
			httptest.WithHeaders(tokenHeaders),
			uploadingManifest(imageOCI.Manifest),
		).ExpectStatus(t, http.StatusCreated)

		// check that the labels_json field is populated correctly in the DB
		expectLabelsJSONOnManifest(
			t, s.DB, image.Manifest.Digest,
			map[string]string{"bar": "is there", "foo": "is there"},
		)
		expectLabelsJSONOnManifest(
			t, s.DB, imageOCI.Manifest.Digest,
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
		// any additional considerations ...
		list := test.GenerateImageList(image, otherImage)
		s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/list",
			httptest.WithHeaders(tokenHeaders),
			uploadingManifest(list.Manifest),
		).ExpectStatus(t, http.StatusCreated)

		// check the labels_json field on the list manifest
		expectLabelsJSONOnManifest(
			t, s.DB, list.Manifest.Digest,
			map[string]string{"foo": "is there"}, // the "bar" label differs between `image` and `otherImage`
		)

		// Test in-toto provenance manifests as an example on how to exclude layer media_types
		layer := test.GenerateExampleLayer(1)
		layer.Annotations = map[string]string{
			"in-toto.io/predicate-type": "https://slsa.dev/provenance/v0.2",
		}
		layer.MediaType = "application/vnd.in-toto+json"
		provenanceManifest := test.GenerateOCIImage(test.OCIArgs{}, layer)
		provenanceManifest.Config.MustUpload(t, s, fooRepoRef)

		s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/list",
			httptest.WithHeaders(tokenHeaders),
			uploadingManifest(provenanceManifest.Manifest),
		).ExpectJSON(t, http.StatusBadRequest, test.ErrorCodeWithMessage{
			Code:    keppel.ErrManifestInvalid,
			Message: "manifest upload {\"labels\":null,\"layers\":[{\"annotations\":{\"in-toto.io/predicate-type\":\"https://slsa.dev/provenance/v0.2\"},\"media_type\":\"application/vnd.in-toto+json\"}],\"media_type\":\"application/vnd.oci.image.manifest.v1+json\",\"repo_name\":\"foo\"} does not satisfy validation rule: 'foo' in labels && 'bar' in labels",
		})

		test.MustExec(t, s.DB,
			`UPDATE accounts SET rule_for_manifest = $1 WHERE name = $2`,
			"'foo' in labels && 'bar' in labels || layers.exists(l, l.media_type == 'application/vnd.in-toto+json')", "test1",
		)

		s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/list",
			httptest.WithHeaders(tokenHeaders),
			uploadingManifest(provenanceManifest.Manifest),
		).ExpectStatus(t, http.StatusCreated)
	})
}

func expectLabelsJSONOnManifest(t *testing.T, db *keppel.DB, manifestDigest digest.Digest, expected map[string]string) {
	t.Helper()
	labelsJSONStr := must.ReturnT(db.SelectStr(`SELECT labels_json FROM manifests WHERE digest = $1`, manifestDigest.String()))(t)

	var actual map[string]string
	must.SucceedT(t, json.Unmarshal([]byte(labelsJSONStr), &actual))
	assert.DeepEqual(t, "labels_json", actual, expected)
}

func TestImageManifestWrongBlobSize(t *testing.T) {
	ctx := t.Context()

	testWithPrimary(t, nil, func(s test.Setup) {
		tokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull,push")

		// generate an image that references a layer, but the reference includes the wrong layer size
		layer := test.GenerateExampleLayer(1)
		layer.MustUpload(t, s, fooRepoRef)

		layer.Contents = append(layer.Contents, []byte("something")...)
		image := test.GenerateImage(layer)
		image.Config.MustUpload(t, s, fooRepoRef)

		s.RespondTo(ctx, "PUT /v2/test1/foo/manifests/latest",
			httptest.WithHeaders(tokenHeaders),
			uploadingManifest(image.Manifest),
		).ExpectJSON(t, http.StatusBadRequest, test.ErrorCode(keppel.ErrManifestInvalid))
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
		image := test.GenerateOCIImage(test.OCIArgs{
			Annotations: map[string]string{"abc": "def"},
		})
		image.MustUpload(t, s, fooRepoRef, "latest")

		// check that the annotations_json field is populated correctly in the DB
		labelsJSONStr := must.ReturnT(s.DB.SelectStr(`SELECT annotations_json FROM manifests WHERE digest = $1`, image.Manifest.Digest.String()))(t)

		var actual map[string]string
		must.Succeed(json.Unmarshal([]byte(labelsJSONStr), &actual))
		assert.DeepEqual(t, "annotations_json", actual, map[string]string{"abc": "def"})
	})
}

func TestManifestArtifactType(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		artifactType := "application/vnd.oci.artifact.config.v1+json"
		image := test.GenerateOCIImage(test.OCIArgs{
			ArtifactType: artifactType,
		})
		image.MustUpload(t, s, fooRepoRef, "latest")

		// check that the annotations_json field is populated correctly in the DB
		actualArtifactType := must.ReturnT(s.DB.SelectStr(`SELECT artifact_type FROM manifests WHERE digest = $1`, image.Manifest.Digest.String()))(t)
		assert.Equal(t, actualArtifactType, artifactType)
	})
}
