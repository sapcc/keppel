// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/httptest"
	"github.com/sapcc/go-bits/must"
	"github.com/sapcc/go-bits/respondwith"
	"go.xyrillian.de/oblast"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestReplicationSimpleImage(t *testing.T) {
	testWithPrimary(t, nil, func(s1 test.Setup) {
		ctx := t.Context()
		// upload image to primary account
		image := test.GenerateImage(test.GenerateExampleLayer(1))
		s1.Clock.StepBy(time.Second)
		image.MustUpload(t, s1, fooRepoRef, "first")

		// test pull by manifest in secondary account
		testWithAllReplicaTypes(t, s1, func(strategy string, firstPass bool, s2 test.Setup) {
			tokenHeaders := s2.GetTokenHeaders(t, "repository:test1/foo:pull")

			if firstPass {
				// replication will not take place while the account is being deleted
				testWithAccountIsDeleting(t, s2.DB, "test1", func() {
					s2.RespondTo(ctx, "GET /v2/test1/foo/manifests/"+image.Manifest.Digest.String(),
						httptest.WithHeaders(tokenHeaders),
					).ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrManifestUnknown))
				})
			} else {
				// if manifest is already present locally, we don't care about the IsDeleting flag
				testWithAccountIsDeleting(t, s2.DB, "test1", func() {
					expectManifestExists(t, s2, tokenHeaders, "test1/foo", image.Manifest, image.Manifest.Digest.String())
				})
			}

			s1.Clock.StepBy(time.Second)
			expectManifestExists(t, s2, tokenHeaders, "test1/foo", image.Manifest, image.Manifest.Digest.String())

			if firstPass && strategy == "on_first_use" {
				easypg.AssertDBContent(t, s2.DB.DB, "fixtures/imagemanifest-replication-001-after-pull-manifest.sql")
			}

			s1.Clock.StepBy(time.Second)
			expectBlobExists(t, s2, tokenHeaders, "test1/foo", image.Config)
			expectBlobExists(t, s2, tokenHeaders, "test1/foo", image.Layers[0])

			if firstPass && strategy == "on_first_use" {
				easypg.AssertDBContent(t, s2.DB.DB, "fixtures/imagemanifest-replication-002-after-pull-blobs.sql")
			}
		})

		// test pull by tag in secondary account
		testWithAllReplicaTypes(t, s1, func(strategy string, firstPass bool, s2 test.Setup) {
			tokenHeaders := s2.GetTokenHeaders(t, "repository:test1/foo:pull")
			expectManifestExists(t, s2, tokenHeaders, "test1/foo", image.Manifest, "first")
			expectBlobExists(t, s2, tokenHeaders, "test1/foo", image.Config)
			expectBlobExists(t, s2, tokenHeaders, "test1/foo", image.Layers[0])
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
			tokenHeaders := s2.GetTokenHeaders(t, "repository:test1/foo:pull")

			if firstPass {
				// do not step the clock in the second pass, otherwise the AssertDBContent
				// will fail on the changed last_pulled_at timestamp
				s1.Clock.StepBy(time.Second)
			}
			expectManifestExists(t, s2, tokenHeaders, "test1/foo", list.Manifest, "list")

			if strategy == "on_first_use" {
				easypg.AssertDBContent(t, s2.DB.DB, "fixtures/imagelistmanifest-replication-001-after-pull-listmanifest.sql")
			}

			if !firstPass {
				// test that this also transferred the referenced manifests eagerly (this
				// part only runs when the primary registry is not reachable)
				expectManifestExists(t, s2, tokenHeaders, "test1/foo", image1.Manifest, "")
				expectManifestExists(t, s2, tokenHeaders, "test1/foo", image2.Manifest, "")
			}
		})
	})
}

func TestReplicationMissingEntities(t *testing.T) {
	testWithPrimary(t, nil, func(s1 test.Setup) {
		ctx := t.Context()
		// ensure that the `test1/foo` repo exists upstream; otherwise we'll just get NAME_UNKNOWN
		_ = must.ReturnT(keppel.FindOrCreateRepository(ctx, s1.DB, "foo", models.AccountName("test1")))(t)

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
			tokenHeaders := s2.GetTokenHeaders(t, "repository:test1/foo:pull")
			s2.RespondTo(ctx, "GET /v2/test1/foo/manifests/thisdoesnotexist",
				httptest.WithHeaders(tokenHeaders),
			).ExpectJSON(t, expectedStatus, test.ErrorCode(expectedManifestError))

			// try to pull a manifest by hash that exists neither locally nor upstream
			bogusDigest := "sha256:" + strings.Repeat("0", 64)
			s2.RespondTo(ctx, "GET /v2/test1/foo/manifests/"+bogusDigest,
				httptest.WithHeaders(tokenHeaders),
			).ExpectJSON(t, expectedStatus, test.ErrorCode(expectedManifestError))

			// try to pull a blob that exists neither locally nor upstream
			// (this always gives 404 because we don't even try to replicate blobs that
			// are not referenced by a manifest that was already replicated)
			s2.RespondTo(ctx, "GET /v2/test1/foo/blobs/"+bogusDigest,
				httptest.WithHeaders(tokenHeaders),
			).ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrBlobUnknown))
		})
	})
}

func TestReplicationForbidDirectUpload(t *testing.T) {
	testWithPrimary(t, nil, func(s1 test.Setup) {
		ctx := t.Context()
		testWithAllReplicaTypes(t, s1, func(strategy string, firstPass bool, s2 test.Setup) {
			tokenHeaders := s2.GetTokenHeaders(t, "repository:test1/foo:pull,push")

			deniedMessage := test.ErrorCodeWithMessage{
				Code:    keppel.ErrUnsupported,
				Message: "cannot push into replica account (push to registry.example.org/test1/foo instead!)",
			}
			if strategy == "from_external_on_first_use" {
				deniedMessage.Message = "cannot push into external replica account (push to registry.example.org/test1/foo instead!)"
			}

			s2.RespondTo(ctx, "POST /v2/test1/foo/blobs/uploads/",
				httptest.WithHeaders(tokenHeaders),
			).ExpectJSON(t, http.StatusMethodNotAllowed, deniedMessage)

			s2.RespondTo(ctx, "PUT /v2/test1/foo/manifests/anotherone",
				httptest.WithHeaders(tokenHeaders),
				httptest.WithBody(strings.NewReader("request body does not matter")),
			).ExpectJSON(t, http.StatusMethodNotAllowed, deniedMessage)
		})
	})
}

func TestReplicationManifestQuotaExceeded(t *testing.T) {
	testWithPrimary(t, nil, func(s1 test.Setup) {
		ctx := t.Context()
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
			test.MustExec(t, s2.DB, `UPDATE quotas SET manifests = $1`, 0)

			quotaExceededMessage := test.ErrorCodeWithMessage{
				Code:    keppel.ErrDenied,
				Message: "manifest quota exceeded (quota = 0, usage = 0)",
			}

			tokenHeaders := s2.GetTokenHeaders(t, "repository:test1/foo:pull")
			s2.RespondTo(ctx, "GET /v2/test1/foo/manifests/first",
				httptest.WithHeaders(tokenHeaders),
				httptest.WithBody(strings.NewReader("request body does not matter")),
			).ExpectJSON(t, http.StatusConflict, quotaExceededMessage)
		})
	})
}

func TestReplicationUseCachedBlobMetadata(t *testing.T) {
	testWithPrimary(t, nil, func(s1 test.Setup) {
		ctx := t.Context()
		// upload image to primary account
		image := test.GenerateImage(test.GenerateExampleLayer(1))
		s1.Clock.StepBy(time.Second)
		image.MustUpload(t, s1, fooRepoRef, "first")

		testWithAllReplicaTypes(t, s1, func(strategy string, firstPass bool, s2 test.Setup) {
			// in the first pass, just replicate the manifest
			tokenHeaders := s2.GetTokenHeaders(t, "repository:test1/foo:pull")
			expectManifestExists(t, s2, tokenHeaders, "test1/foo", image.Manifest, "first")

			// in the second pass, query blobs with HEAD - this should work fine even
			// though the blob contents are not replicated since all necessary metadata
			// can be obtained from the manifest
			for _, blob := range []test.Bytes{image.Config, image.Layers[0]} {
				s2.RespondTo(ctx, "HEAD /v2/test1/foo/blobs/"+blob.Digest.String(),
					httptest.WithHeaders(tokenHeaders),
				).ExpectHeaders(t, http.Header{
					"Content-Length":        {strconv.Itoa(len(blob.Contents))},
					"Content-Type":          {blob.MediaType},
					"Docker-Content-Digest": {blob.Digest.String()},
				}).ExpectStatus(t, http.StatusOK)
			}
		})
	})
}

func TestReplicationForbidAnonymousReplicationFromExternal(t *testing.T) {
	testWithPrimary(t, nil, func(s1 test.Setup) {
		ctx := t.Context()
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

			// enable anonymous pull on the account
			test.MustExec(t, s2.DB, `UPDATE accounts SET rbac_policies_json = $2 WHERE name = $1`, "test1",
				test.ToJSON([]keppel.RBACPolicy{{
					RepositoryPattern: ".*",
					Permissions:       []keppel.RBACPermission{keppel.RBACAnonymousPullPermission},
				}}),
			)

			// replicating pull is forbidden with an anonymous token
			// (error message option 1 for the "repo does not exist" case)
			anonTokenHeaders := s2.GetAnonTokenHeaders(t, "repository:test1/foo", []string{"pull"})
			s2.RespondTo(ctx, "GET /v2/test1/foo/manifests/first", httptest.WithHeaders(anonTokenHeaders)).
				ExpectHeader(t, "Www-Authenticate", `Bearer realm="https://registry-secondary.example.org/keppel/v1/auth",service="registry-secondary.example.org",scope="repository:test1/foo:pull"`).
				ExpectJSON(t, http.StatusUnauthorized, test.ErrorCodeWithMessage{
					Code:    keppel.ErrDenied,
					Message: "repository does not exist here, and anonymous users may not create new repositories",
				})

			// create the "test1/foo" repo on secondary to show the other error option
			tokenHeaders := s2.GetTokenHeaders(t, "repository:test1/foo:pull")
			expectManifestExists(t, s2, tokenHeaders, "test1/foo", image.Manifest, "second")

			// replicating pull is forbidden with an anonymous token...
			// (error message option 2 for the "repo does exist, but image does not" case)
			s2.RespondTo(ctx, "GET /v2/test1/foo/manifests/first", httptest.WithHeaders(anonTokenHeaders)).
				ExpectHeader(t, "Www-Authenticate", `Bearer realm="https://registry-secondary.example.org/keppel/v1/auth",service="registry-secondary.example.org",scope="repository:test1/foo:pull"`).
				ExpectJSON(t, http.StatusUnauthorized, test.ErrorCodeWithMessage{
					Code:    keppel.ErrDenied,
					Message: "image does not exist here, and anonymous users may not replicate images",
				})

			// ...but allowed with a non-anonymous token...
			expectManifestExists(t, s2, tokenHeaders, "test1/foo", image.Manifest, "first")
			// ...and once replicated, the anonymous token can pull as well
			expectManifestExists(t, s2, anonTokenHeaders, "test1/foo", image.Manifest, "first")
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

			// enable anonymous pull and replication on test1/bar
			test.MustExec(t, s2.DB, `UPDATE accounts SET rbac_policies_json = $2 WHERE name = $1`, "test1",
				test.ToJSON([]keppel.RBACPolicy{{
					RepositoryPattern: "foo",
					Permissions:       []keppel.RBACPermission{keppel.RBACAnonymousPullPermission, keppel.RBACAnonymousFirstPullPermission},
				}}),
			)

			// the rbac policy allows to replicate test1/foo images
			anonTokenHeaders := s2.GetAnonTokenHeaders(t, "repository:test1/foo", []string{"pull", "anonymous_first_pull"})
			expectManifestExists(t, s2, anonTokenHeaders, "test1/foo", image.Manifest, "first")
		})
	})
}

func TestReplicationImageListWithPlatformFilter(t *testing.T) {
	testWithPrimary(t, nil, func(s1 test.Setup) {
		ctx := t.Context()
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
			test.MustExec(t, s2.DB, `UPDATE accounts SET platform_filter = $1`, `[{"os":"linux","architecture":"amd64"}]`)

			tokenHeaders := s2.GetTokenHeaders(t, "repository:test1/foo:pull")

			if firstPass {
				// do not step the clock in the second pass, otherwise the AssertDBContent
				// will fail on the changed last_pulled_at timestamp
				s1.Clock.StepBy(time.Second)
			}
			expectManifestExists(t, s2, tokenHeaders, "test1/foo", list.Manifest, "list")

			if strategy == "on_first_use" {
				easypg.AssertDBContent(t, s2.DB.DB, "fixtures/imagelistmanifest-replication-with-platformfilter-001-after-pull-listmanifest.sql")
			}

			if !firstPass {
				// test that this also transferred the referenced manifests eagerly (this
				// part only runs when the primary registry is not reachable)
				expectManifestExists(t, s2, tokenHeaders, "test1/foo", image1.Manifest, "")

				// when now requesting the unreplicated manifest, the replica will try
				// to replicate and therefore run into a network error
				s2.RespondTo(ctx, "GET /v2/test1/foo/manifests/"+image2.Manifest.Digest.String(),
					httptest.WithHeaders(tokenHeaders),
				).ExpectJSON(t, http.StatusServiceUnavailable, test.ErrorCode(keppel.ErrUnavailable))
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
		ctx := t.Context()
		testWithReplica(t, s1, "on_first_use", func(firstPass bool, s2 test.Setup) {
			if !firstPass {
				return // no second pass needed
			}

			tokenHeaders1 := s1.GetTokenHeaders(t, "repository:test1/foo:pull")
			tokenHeaders2 := s2.GetTokenHeaders(t, "repository:test1/foo:pull")

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
			for _, db := range []*oblast.DB{s1.DB, s2.DB} {
				test.MustExec(t, db, `UPDATE accounts SET upstream_peer_hostname = '', external_peer_url = $2 WHERE name = $1`,
					"test1", "registry-tertiary.example.org")
			}

			// test successful pull delegation (from secondary to primary)
			requestCounter = 0
			s2.RespondTo(ctx, "GET /v2/test1/foo/manifests/"+image.Manifest.Digest.String(),
				httptest.WithHeaders(tokenHeaders2),
			).ExpectBody(t, http.StatusOK, image.Manifest.Contents)

			// test failed pull delegation (from primary to secondary; primary does
			// not have a password for secondary's peering API, so delegation fails
			// and we see the original 429 response instead)
			requestCounter = 0
			s1.RespondTo(ctx, "GET /v2/test1/foo/manifests/"+image.Manifest.Digest.String(),
				httptest.WithHeaders(tokenHeaders1),
			).ExpectJSON(t, http.StatusTooManyRequests, test.ErrorCode(keppel.ErrTooManyRequests))
		})
	})
}

func TestReplicationFailingFromHarbor(t *testing.T) {
	testWithPrimary(t, []test.SetupOption{test.WithPeerAPI}, func(s1 test.Setup) {
		ctx := t.Context()
		// setup second as a mostly static responder
		http.DefaultTransport.(*test.RoundTripper).Handlers["registry-secondary.example.org"] = http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				keppel.ErrNonStandardHarborNotFound.WithError(errors.New("simulated failure from Harbor registry")).WriteAsRegistryV2ResponseTo(w, r)
			},
		)

		test.MustExec(t, s1.DB, `UPDATE accounts SET upstream_peer_hostname = '', external_peer_url = $2 WHERE name = $1`,
			"test1", "registry-secondary.example.org")

		s1.RespondTo(ctx, "GET /v2/test1/foo/manifests/latest",
			httptest.WithHeaders(s1.GetTokenHeaders(t, "repository:test1/foo:pull")),
		).ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrNonStandardHarborNotFound))
	})
}

func TestReplicationFailingFromGHCRio(t *testing.T) {
	testWithPrimary(t, []test.SetupOption{test.WithPeerAPI}, func(s1 test.Setup) {
		ctx := t.Context()
		// setup second as a mostly static responder
		http.DefaultTransport.(*test.RoundTripper).Handlers["registry-secondary.example.org"] = http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v2/foo/manifests/latest":
					w.Header().Add("Www-Authenticate", `Bearer realm="http://registry-secondary.example.org/token",service="registry-secondary.example.org",scope="repository:foo:pull"`)
					keppel.ErrUnauthorized.WithError(errors.New("simulated unauthorized from GHCR.io")).WriteAsRegistryV2ResponseTo(w, r)
				case "/token":
					keppel.ErrDenied.WithError(errors.New("requested access to the resource is denied")).WriteAsRegistryV2ResponseTo(w, r)
				default:
					http.NotFound(w, r)
				}
			},
		)

		test.MustExec(t, s1.DB, `UPDATE accounts SET upstream_peer_hostname = '', external_peer_url = $2 WHERE name = $1`,
			"test1", "registry-secondary.example.org")

		s1.RespondTo(ctx, "GET /v2/test1/foo/manifests/latest",
			httptest.WithHeaders(s1.GetTokenHeaders(t, "repository:test1/foo:pull")),
		).
			// even though we return 401, no Www-Authenticate header shall be rendered because would be futile for the user performing this request to authenticate
			ExpectHeader(t, "Www-Authenticate", "").
			ExpectJSON(t, http.StatusUnauthorized, test.ErrorCode(keppel.ErrDenied))
	})
}

func TestReplicationSuccessfulFromNVCRio(t *testing.T) {
	testWithPrimary(t, []test.SetupOption{test.WithPeerAPI}, func(s1 test.Setup) {
		ctx := t.Context()

		// setup a mock responder that acts like nvcr.io in the relevant ways
		http.DefaultTransport.(*test.RoundTripper).Handlers["nvcr.io"] = http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v2/foo/manifests/latest":
					if r.Header.Get("Authorization") == "Bearer foobar" {
						// for this test, we only care that the authentication workflow succeeds;
						// after that, we will still report a 404 because that is the easiest to do
						keppel.ErrManifestUnknown.WithError(errors.New("manifest unknown")).WriteAsRegistryV2ResponseTo(w, r)
					} else {
						// nvcr.io's Www-Authenticate does not include a service parameter
						// (this is the important part of this test, because Keppel used to choke on that)
						w.Header().Add("Www-Authenticate", `Bearer realm="http://nvcr.io/proxy_auth",scope="repository:foo:pull"`)
						// also its 401 responses do not contain RegistryV2Error payloads
						w.Header().Set("Content-Type", "text/html")
						w.WriteHeader(http.StatusUnauthorized)
						fmt.Fprint(w, `<html><head><title>401 Authorization Required</title></head><body><center><h1>401 Authorization Required</h1></center><hr><center>nginx/1.22.1</center></body></html>`)
					}
				case "/proxy_auth":
					respondwith.JSON(w, http.StatusOK, map[string]any{"token": "foobar", "expires_in": 500})
				default:
					http.NotFound(w, r)
				}
			},
		)

		test.MustExec(t, s1.DB, `UPDATE accounts SET upstream_peer_hostname = '', external_peer_url = $2 WHERE name = $1`,
			"test1", "nvcr.io")

		s1.RespondTo(ctx, "GET /v2/test1/foo/manifests/latest",
			httptest.WithHeaders(s1.GetTokenHeaders(t, "repository:test1/foo:pull")),
		).ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrManifestUnknown))
	})
}
