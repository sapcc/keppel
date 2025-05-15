// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestReplicationSimpleImage(t *testing.T) {
	testWithPrimary(t, nil, func(s1 test.Setup) {
		// upload image to primary account
		image := test.GenerateImage(test.GenerateExampleLayer(1))
		s1.Clock.StepBy(time.Second)
		image.MustUpload(t, s1, fooRepoRef, "first")

		// test pull by manifest in secondary account
		testWithAllReplicaTypes(t, s1, func(strategy string, firstPass bool, s2 test.Setup) {
			h2 := s2.Handler
			token := s2.GetToken(t, "repository:test1/foo:pull")

			if firstPass {
				// replication will not take place while the account is in maintenance
				testWithAccountIsDeleting(t, s2.DB, "test1", func() {
					assert.HTTPRequest{
						Method:       "GET",
						Path:         "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
						Header:       map[string]string{"Authorization": "Bearer " + token},
						ExpectStatus: http.StatusNotFound,
						ExpectHeader: test.VersionHeader,
						ExpectBody:   test.ErrorCode(keppel.ErrManifestUnknown),
					}.Check(t, h2)
				})
			} else {
				// if manifest is already present locally, we don't care about the maintenance mode
				testWithAccountIsDeleting(t, s2.DB, "test1", func() {
					expectManifestExists(t, h2, token, "test1/foo", image.Manifest, image.Manifest.Digest.String(), nil)
				})
			}

			s1.Clock.StepBy(time.Second)
			expectManifestExists(t, h2, token, "test1/foo", image.Manifest, image.Manifest.Digest.String(), nil)

			if firstPass && strategy == "on_first_use" {
				easypg.AssertDBContent(t, s2.DB.Db, "fixtures/imagemanifest-replication-001-after-pull-manifest.sql")
			}

			s1.Clock.StepBy(time.Second)
			expectBlobExists(t, h2, token, "test1/foo", image.Config, nil)
			expectBlobExists(t, h2, token, "test1/foo", image.Layers[0], nil)

			if firstPass && strategy == "on_first_use" {
				easypg.AssertDBContent(t, s2.DB.Db, "fixtures/imagemanifest-replication-002-after-pull-blobs.sql")
			}
		})

		// test pull by tag in secondary account
		testWithAllReplicaTypes(t, s1, func(strategy string, firstPass bool, s2 test.Setup) {
			h2 := s2.Handler
			token := s2.GetToken(t, "repository:test1/foo:pull")
			expectManifestExists(t, h2, token, "test1/foo", image.Manifest, "first", nil)
			expectBlobExists(t, h2, token, "test1/foo", image.Config, nil)
			expectBlobExists(t, h2, token, "test1/foo", image.Layers[0], nil)
		})
	})
}

func TestReplicationImageList(t *testing.T) {
	testWithPrimary(t, nil, func(s1 test.Setup) {
		// upload image list with two images to primary account
		image1 := test.GenerateImage(test.GenerateExampleLayer(1))
		image2 := test.GenerateImage(test.GenerateExampleLayer(2))
		list := test.GenerateImageList(image1, image2)
		s1.Clock.StepBy(time.Second)
		image1.MustUpload(t, s1, fooRepoRef, "first")
		image2.MustUpload(t, s1, fooRepoRef, "second")
		list.MustUpload(t, s1, fooRepoRef, "list")

		// test pull in secondary account
		testWithAllReplicaTypes(t, s1, func(strategy string, firstPass bool, s2 test.Setup) {
			h2 := s2.Handler
			token := s2.GetToken(t, "repository:test1/foo:pull")

			if firstPass {
				// do not step the clock in the second pass, otherwise the AssertDBContent
				// will fail on the changed last_pulled_at timestamp
				s1.Clock.StepBy(time.Second)
			}
			expectManifestExists(t, h2, token, "test1/foo", list.Manifest, "list", nil)

			if strategy == "on_first_use" {
				easypg.AssertDBContent(t, s2.DB.Db, "fixtures/imagelistmanifest-replication-001-after-pull-listmanifest.sql")
			}

			if !firstPass {
				// test that this also transferred the referenced manifests eagerly (this
				// part only runs when the primary registry is not reachable)
				expectManifestExists(t, h2, token, "test1/foo", image1.Manifest, "", nil)
				expectManifestExists(t, h2, token, "test1/foo", image2.Manifest, "", nil)
			}
		})
	})
}

func TestReplicationMissingEntities(t *testing.T) {
	testWithPrimary(t, nil, func(s1 test.Setup) {
		// ensure that the `test1/foo` repo exists upstream; otherwise we'll just get
		// NAME_UNKNOWN
		_, err := keppel.FindOrCreateRepository(s1.DB, "foo", models.AccountName("test1"))
		if err != nil {
			t.Fatal(err.Error())
		}

		testWithAllReplicaTypes(t, s1, func(strategy string, firstPass bool, s2 test.Setup) {
			var (
				expectedStatus        = http.StatusNotFound
				expectedManifestError = keppel.ErrManifestUnknown
			)
			if !firstPass {
				// in the second pass, when the upstream registry is not reachable, we will get network errors instead
				expectedStatus = http.StatusServiceUnavailable
				expectedManifestError = keppel.ErrUnavailable
			}

			// try to pull a manifest by tag that exists neither locally nor upstream
			h2 := s2.Handler
			token := s2.GetToken(t, "repository:test1/foo:pull")
			assert.HTTPRequest{
				Method:       "GET",
				Path:         "/v2/test1/foo/manifests/thisdoesnotexist",
				Header:       map[string]string{"Authorization": "Bearer " + token},
				ExpectStatus: expectedStatus,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(expectedManifestError),
			}.Check(t, h2)

			// try to pull a manifest by hash that exists neither locally nor upstream
			bogusDigest := "sha256:" + strings.Repeat("0", 64)
			assert.HTTPRequest{
				Method:       "GET",
				Path:         "/v2/test1/foo/manifests/" + bogusDigest,
				Header:       map[string]string{"Authorization": "Bearer " + token},
				ExpectStatus: expectedStatus,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(expectedManifestError),
			}.Check(t, h2)

			// try to pull a blob that exists neither locally nor upstream
			// (this always gives 404 because we don't even try to replicate blobs that
			// are not referenced by a manifest that was already replicated)
			assert.HTTPRequest{
				Method:       "GET",
				Path:         "/v2/test1/foo/blobs/" + bogusDigest,
				Header:       map[string]string{"Authorization": "Bearer " + token},
				ExpectStatus: http.StatusNotFound,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrBlobUnknown),
			}.Check(t, h2)
		})
	})
}

func TestReplicationForbidDirectUpload(t *testing.T) {
	testWithPrimary(t, nil, func(s1 test.Setup) {
		testWithAllReplicaTypes(t, s1, func(strategy string, firstPass bool, s2 test.Setup) {
			h2 := s2.Handler
			token := s2.GetToken(t, "repository:test1/foo:pull,push")

			deniedMessage := test.ErrorCodeWithMessage{
				Code:    keppel.ErrUnsupported,
				Message: "cannot push into replica account (push to registry.example.org/test1/foo instead!)",
			}
			if strategy == "from_external_on_first_use" {
				deniedMessage.Message = "cannot push into external replica account (push to registry.example.org/test1/foo instead!)"
			}

			assert.HTTPRequest{
				Method:       "POST",
				Path:         "/v2/test1/foo/blobs/uploads/",
				Header:       map[string]string{"Authorization": "Bearer " + token},
				ExpectStatus: http.StatusMethodNotAllowed,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   deniedMessage,
			}.Check(t, h2)

			assert.HTTPRequest{
				Method:       "PUT",
				Path:         "/v2/test1/foo/manifests/anotherone",
				Header:       map[string]string{"Authorization": "Bearer " + token},
				Body:         assert.StringData("request body does not matter"),
				ExpectStatus: http.StatusMethodNotAllowed,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   deniedMessage,
			}.Check(t, h2)
		})
	})
}

func TestReplicationManifestQuotaExceeded(t *testing.T) {
	testWithPrimary(t, nil, func(s1 test.Setup) {
		// upload image to primary account
		image := test.GenerateImage(test.GenerateExampleLayer(1))
		s1.Clock.StepBy(time.Second)
		image.MustUpload(t, s1, fooRepoRef, "first")

		// in secondary account...
		testWithAllReplicaTypes(t, s1, func(strategy string, firstPass bool, s2 test.Setup) {
			if !firstPass {
				return
			}

			// ...lower quotas so that replication will fail
			_, err := s2.DB.Exec(`UPDATE quotas SET manifests = $1`, 0)
			if err != nil {
				t.Fatal(err.Error())
			}

			quotaExceededMessage := test.ErrorCodeWithMessage{
				Code:    keppel.ErrDenied,
				Message: "manifest quota exceeded (quota = 0, usage = 0)",
			}

			h2 := s2.Handler
			token := s2.GetToken(t, "repository:test1/foo:pull")
			assert.HTTPRequest{
				Method:       "GET",
				Path:         "/v2/test1/foo/manifests/first",
				Header:       map[string]string{"Authorization": "Bearer " + token},
				Body:         assert.StringData("request body does not matter"),
				ExpectStatus: http.StatusConflict,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   quotaExceededMessage,
			}.Check(t, h2)
		})
	})
}

func TestReplicationUseCachedBlobMetadata(t *testing.T) {
	testWithPrimary(t, nil, func(s1 test.Setup) {
		// upload image to primary account
		image := test.GenerateImage(test.GenerateExampleLayer(1))
		s1.Clock.StepBy(time.Second)
		image.MustUpload(t, s1, fooRepoRef, "first")

		testWithAllReplicaTypes(t, s1, func(strategy string, firstPass bool, s2 test.Setup) {
			// in the first pass, just replicate the manifest
			h2 := s2.Handler
			token := s2.GetToken(t, "repository:test1/foo:pull")
			expectManifestExists(t, h2, token, "test1/foo", image.Manifest, "first", nil)

			// in the second pass, query blobs with HEAD - this should work fine even
			// though the blob contents are not replicated since all necessary metadata
			// can be obtained from the manifest
			for _, blob := range []test.Bytes{image.Config, image.Layers[0]} {
				assert.HTTPRequest{
					Method:       "HEAD",
					Path:         "/v2/test1/foo/blobs/" + blob.Digest.String(),
					Header:       map[string]string{"Authorization": "Bearer " + token},
					ExpectStatus: http.StatusOK,
					ExpectHeader: map[string]string{
						test.VersionHeaderKey:   test.VersionHeaderValue,
						"Content-Length":        strconv.Itoa(len(blob.Contents)),
						"Content-Type":          blob.MediaType,
						"Docker-Content-Digest": blob.Digest.String(),
					},
				}.Check(t, h2)
			}
		})
	})
}

func TestReplicationForbidAnonymousReplicationFromExternal(t *testing.T) {
	testWithPrimary(t, nil, func(s1 test.Setup) {
		// upload image to primary account
		image := test.GenerateImage(test.GenerateExampleLayer(1))
		s1.Clock.StepBy(time.Second)
		image.MustUpload(t, s1, fooRepoRef, "first")
		image.MustUpload(t, s1, fooRepoRef, "second")

		testWithReplica(t, s1, "from_external_on_first_use", func(firstPass bool, s2 test.Setup) {
			// need only one pass for this test
			if !firstPass {
				return
			}

			// make sure that the "test1/foo" repo exists on secondary (otherwise we
			// will get useless NAME_UNKNOWN errors later, not the errors we're interested in)
			h2 := s2.Handler
			token := s2.GetToken(t, "repository:test1/foo:pull")
			expectManifestExists(t, h2, token, "test1/foo", image.Manifest, "second", nil)

			// enable anonymous pull on the account
			_, err := s2.DB.Exec(`UPDATE accounts SET rbac_policies_json = $2 WHERE name = $1`, "test1",
				test.ToJSON([]keppel.RBACPolicy{{
					RepositoryPattern: ".*",
					Permissions:       []keppel.RBACPermission{keppel.RBACAnonymousPullPermission},
				}}),
			)
			if err != nil {
				t.Fatal(err.Error())
			}

			// get an anonymous token (this is a bit unwieldy because usually all
			// tests work with non-anonymous tokens, so we don't have helper functions
			// for anonymous tokens)
			_, tokenBodyBytes := assert.HTTPRequest{
				Method: "GET",
				Path:   "/keppel/v1/auth?service=registry-secondary.example.org&scope=repository:test1/foo:pull",
				Header: map[string]string{
					"X-Forwarded-Host":  "registry-secondary.example.org",
					"X-Forwarded-Proto": "https",
				},
				ExpectStatus: http.StatusOK,
			}.Check(t, h2)
			var tokenBodyData struct {
				Token string `json:"token"`
			}
			err = json.Unmarshal(tokenBodyBytes, &tokenBodyData)
			if err != nil {
				t.Fatal(err.Error())
			}
			anonToken := tokenBodyData.Token

			// replicating pull is forbidden with an anonymous token...
			assert.HTTPRequest{
				Method:       "GET",
				Path:         "/v2/test1/foo/manifests/first",
				Header:       map[string]string{"Authorization": "Bearer " + anonToken},
				ExpectHeader: map[string]string{"Www-Authenticate": `Bearer realm="http://example.com/keppel/v1/auth",service="registry-secondary.example.org",scope="repository:test1/foo:pull"`},
				ExpectStatus: http.StatusUnauthorized,
				ExpectBody: test.ErrorCodeWithMessage{
					Code:    keppel.ErrDenied,
					Message: "image does not exist here, and anonymous users may not replicate images",
				},
			}.Check(t, h2)

			// ...but allowed with a non-anonymous token...
			expectManifestExists(t, h2, token, "test1/foo", image.Manifest, "first", nil)
			// ...and once replicated, the anonymous token can pull as well
			expectManifestExists(t, h2, anonToken, "test1/foo", image.Manifest, "first", nil)
		})
	})
}

func TestReplicationAllowAnonymousReplicationFromExternal(t *testing.T) {
	testWithPrimary(t, nil, func(s1 test.Setup) {
		// upload image to primary account
		image := test.GenerateImage(test.GenerateExampleLayer(1))
		s1.Clock.StepBy(time.Second)
		image.MustUpload(t, s1, fooRepoRef, "first")

		testWithReplica(t, s1, "from_external_on_first_use", func(firstPass bool, s2 test.Setup) {
			// need only one pass for this test
			if !firstPass {
				return
			}

			h2 := s2.Handler

			// enable anonymous pull and replication on test1/bar
			_, err := s2.DB.Exec(`UPDATE accounts SET rbac_policies_json = $2 WHERE name = $1`, "test1",
				test.ToJSON([]keppel.RBACPolicy{{
					RepositoryPattern: "foo",
					Permissions:       []keppel.RBACPermission{keppel.RBACAnonymousPullPermission, keppel.RBACAnonymousFirstPullPermission},
				}}),
			)
			if err != nil {
				t.Fatal(err.Error())
			}

			// get an anonymous token (this is a bit unwieldy because usually all
			// tests work with non-anonymous tokens, so we don't have helper functions
			// for anonymous tokens)
			// TODO: extract to s1.getAnonToken(t, "repository:test1/foo:pull")
			_, tokenBodyBytes := assert.HTTPRequest{
				Method: "GET",
				Path:   "/keppel/v1/auth?service=registry-secondary.example.org&scope=repository:test1/foo:pull,anonymous_first_pull",
				Header: map[string]string{
					"X-Forwarded-Host":  "registry-secondary.example.org",
					"X-Forwarded-Proto": "https",
				},
				ExpectStatus: http.StatusOK,
			}.Check(t, h2)
			var tokenBodyData struct {
				Token string `json:"token"`
			}
			err = json.Unmarshal(tokenBodyBytes, &tokenBodyData)
			if err != nil {
				t.Fatal(err.Error())
			}
			anonToken := tokenBodyData.Token

			// the rbac policy allows to replicate test1/foo images
			expectManifestExists(t, h2, anonToken, "test1/foo", image.Manifest, "first", nil)
		})
	})
}

func TestReplicationImageListWithPlatformFilter(t *testing.T) {
	testWithPrimary(t, nil, func(s1 test.Setup) {
		// This test is mostly identical to TestReplicationImageList(), but the
		// replica will get a platform_filter and thus not replicate all
		// submanifests.
		image1 := test.GenerateImage(test.GenerateExampleLayer(1))
		image2 := test.GenerateImage(test.GenerateExampleLayer(2))
		list := test.GenerateImageList(image1, image2)
		s1.Clock.StepBy(time.Second)
		image1.MustUpload(t, s1, fooRepoRef, "first")
		image2.MustUpload(t, s1, fooRepoRef, "second")
		list.MustUpload(t, s1, fooRepoRef, "list")

		// test pull in secondary account
		testWithAllReplicaTypes(t, s1, func(strategy string, firstPass bool, s2 test.Setup) {
			// setup the platform_filter that differentiates this test from
			// TestReplicationImageList()
			_, err := s2.DB.Exec(`UPDATE accounts SET platform_filter = $1`, `[{"os":"linux","architecture":"amd64"}]`)
			if err != nil {
				t.Fatal(err.Error())
			}

			h2 := s2.Handler
			token := s2.GetToken(t, "repository:test1/foo:pull")

			if firstPass {
				// do not step the clock in the second pass, otherwise the AssertDBContent
				// will fail on the changed last_pulled_at timestamp
				s1.Clock.StepBy(time.Second)
			}
			expectManifestExists(t, h2, token, "test1/foo", list.Manifest, "list", nil)

			if strategy == "on_first_use" {
				easypg.AssertDBContent(t, s2.DB.Db, "fixtures/imagelistmanifest-replication-with-platformfilter-001-after-pull-listmanifest.sql")
			}

			if !firstPass {
				// test that this also transferred the referenced manifests eagerly (this
				// part only runs when the primary registry is not reachable)
				expectManifestExists(t, h2, token, "test1/foo", image1.Manifest, "", nil)

				// when now requesting the unreplicated manifest, the replica will try
				// to replicate and therefore run into a network error
				assert.HTTPRequest{
					Method:       "GET",
					Path:         "/v2/test1/foo/manifests/" + image2.Manifest.Digest.String(),
					Header:       map[string]string{"Authorization": "Bearer " + token},
					ExpectStatus: http.StatusServiceUnavailable,
					ExpectHeader: test.VersionHeader,
					ExpectBody:   test.ErrorCode(keppel.ErrUnavailable),
				}.Check(t, h2)
			}
		})
	})
}

func TestReplicationFailingOverIntoPullDelegation(t *testing.T) {
	// This test is more contrived than the others because we have *three* registries involved instead of two.
	//- Primary and secondary are, as usual, set up as peers of each other with a replicated account "test1".
	//- "test1" is reconfigured into an external replica of tertiary on both primary and secondary.
	//- Tertiary rejects the first pull to trigger the pull delegation code path.

	setupOptions := []test.SetupOption{
		test.WithPeerAPI,
	}

	testWithPrimary(t, setupOptions, func(s1 test.Setup) {
		testWithReplica(t, s1, "on_first_use", func(firstPass bool, s2 test.Setup) {
			if !firstPass {
				return // no second pass needed
			}

			h1 := s1.Handler
			h2 := s2.Handler
			token1 := s1.GetToken(t, "repository:test1/foo:pull")
			token2 := s2.GetToken(t, "repository:test1/foo:pull")

			// setup tertiary as a mostly static responder
			image := test.GenerateImage(test.GenerateExampleLayer(1))
			requestCounter := 0
			tertiaryHandler := func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					http.Error(w, r.Method+"not allowed", http.StatusMethodNotAllowed)
					return
				}
				for _, blob := range append(image.Layers, image.Config) {
					if r.URL.Path == "/v2/foo/blobs/"+blob.Digest.String() {
						w.Header().Set("Content-Length", strconv.Itoa(len(blob.Contents)))
						w.WriteHeader(http.StatusOK)
						w.Write(blob.Contents)
						return
					}
				}
				if r.URL.Path == "/v2/foo/manifests/"+image.Manifest.Digest.String() {
					requestCounter++
					if requestCounter == 1 {
						// reject initial manifest pull to trigger the pull delegation code path
						keppel.ErrTooManyRequests.With("").WriteAsRegistryV2ResponseTo(w, r)
						return
					}
					w.Header().Set("Content-Type", image.Manifest.MediaType)
					w.Header().Set("Content-Length", strconv.Itoa(len(image.Manifest.Contents)))
					w.WriteHeader(http.StatusOK)
					w.Write(image.Manifest.Contents)
					return
				}
			}
			http.DefaultTransport.(*test.RoundTripper).Handlers["registry-tertiary.example.org"] = http.HandlerFunc(tertiaryHandler)

			// reconfigure "test1" into an external replica of tertiary
			for _, db := range []*keppel.DB{s1.DB, s2.DB} {
				_, err := db.Exec(`UPDATE accounts SET upstream_peer_hostname = '', external_peer_url = $2 WHERE name = $1`,
					"test1", "registry-tertiary.example.org")
				if err != nil {
					t.Fatal(err.Error())
				}
			}

			// test successful pull delegation (from secondary to primary)
			requestCounter = 0
			assert.HTTPRequest{
				Method:       "GET",
				Path:         "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
				Header:       map[string]string{"Authorization": "Bearer " + token2},
				ExpectStatus: http.StatusOK,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   assert.ByteData(image.Manifest.Contents),
			}.Check(t, h2)

			// test failed pull delegation (from primary to secondary; primary does
			// not have a password for secondary's peering API, so delegation fails
			// and we see the original 429 response instead)
			requestCounter = 0
			assert.HTTPRequest{
				Method:       "GET",
				Path:         "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
				Header:       map[string]string{"Authorization": "Bearer " + token1},
				ExpectStatus: http.StatusTooManyRequests,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrTooManyRequests),
			}.Check(t, h1)
		})
	})
}
