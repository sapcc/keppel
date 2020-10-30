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
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestReplicationSimpleImage(t *testing.T) {
	testWithPrimary(t, nil, func(h1 http.Handler, cfg1 keppel.Configuration, db1 *keppel.DB, ad1 *test.AuthDriver, sd1 *test.StorageDriver, fd1 *test.FederationDriver, clock *test.Clock) {
		//upload image to primary account
		token := getToken(t, h1, ad1, "repository:test1/foo:pull,push",
			keppel.CanPullFromAccount,
			keppel.CanPushToAccount)
		image := test.GenerateImage(test.GenerateExampleLayer(1))
		clock.Step()
		uploadBlob(t, h1, token, "test1/foo", image.Layers[0])
		uploadBlob(t, h1, token, "test1/foo", image.Config)
		uploadManifest(t, h1, token, "test1/foo", image.Manifest, "first")

		//test pull by manifest in secondary account
		testWithAllReplicaTypes(t, h1, db1, clock, func(strategy string, firstPass bool, h2 http.Handler, cfg2 keppel.Configuration, db2 *keppel.DB, ad2 *test.AuthDriver, sd2 *test.StorageDriver) {
			token := getTokenForSecondary(t, h2, ad2, "repository:test1/foo:pull",
				keppel.CanPullFromAccount)

			if firstPass {
				//replication will not take place while the account is in maintenance
				testWithAccountInMaintenance(t, db2, "test1", func() {
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
				//if manifest is already present locally, we don't care about the maintenance mode
				testWithAccountInMaintenance(t, db2, "test1", func() {
					expectManifestExists(t, h2, token, "test1/foo", image.Manifest, image.Manifest.Digest.String(), nil)
				})
			}

			clock.Step()
			expectManifestExists(t, h2, token, "test1/foo", image.Manifest, image.Manifest.Digest.String(), nil)

			if firstPass && strategy == "on_first_use" {
				easypg.AssertDBContent(t, db2.DbMap.Db, "fixtures/imagemanifest-replication-001-after-pull-manifest.sql")
			}

			clock.Step()
			expectBlobExists(t, h2, token, "test1/foo", image.Config, nil)
			expectBlobExists(t, h2, token, "test1/foo", image.Layers[0], nil)

			if firstPass && strategy == "on_first_use" {
				easypg.AssertDBContent(t, db2.DbMap.Db, "fixtures/imagemanifest-replication-002-after-pull-blobs.sql")
			}
		})

		//test pull by tag in secondary account
		testWithAllReplicaTypes(t, h1, db1, clock, func(strategy string, firstPass bool, h2 http.Handler, cfg2 keppel.Configuration, db2 *keppel.DB, ad2 *test.AuthDriver, sd2 *test.StorageDriver) {
			token := getTokenForSecondary(t, h2, ad2, "repository:test1/foo:pull",
				keppel.CanPullFromAccount)
			expectManifestExists(t, h2, token, "test1/foo", image.Manifest, "first", nil)
			expectBlobExists(t, h2, token, "test1/foo", image.Config, nil)
			expectBlobExists(t, h2, token, "test1/foo", image.Layers[0], nil)
		})
	})
}

func TestReplicationImageList(t *testing.T) {
	testWithPrimary(t, nil, func(h1 http.Handler, cfg1 keppel.Configuration, db1 *keppel.DB, ad1 *test.AuthDriver, sd1 *test.StorageDriver, fd1 *test.FederationDriver, clock *test.Clock) {
		//upload image list with two images to primary account
		token := getToken(t, h1, ad1, "repository:test1/foo:pull,push",
			keppel.CanPullFromAccount,
			keppel.CanPushToAccount)
		image1 := test.GenerateImage(test.GenerateExampleLayer(1))
		image2 := test.GenerateImage(test.GenerateExampleLayer(2))
		list := test.GenerateImageList(image1.Manifest, image2.Manifest)
		clock.Step()
		uploadBlob(t, h1, token, "test1/foo", image1.Layers[0])
		uploadBlob(t, h1, token, "test1/foo", image1.Config)
		uploadManifest(t, h1, token, "test1/foo", image1.Manifest, "first")
		uploadBlob(t, h1, token, "test1/foo", image2.Layers[0])
		uploadBlob(t, h1, token, "test1/foo", image2.Config)
		uploadManifest(t, h1, token, "test1/foo", image2.Manifest, "second")
		uploadManifest(t, h1, token, "test1/foo", list.Manifest, "list")

		//test pull in secondary account
		testWithAllReplicaTypes(t, h1, db1, clock, func(strategy string, firstPass bool, h2 http.Handler, cfg2 keppel.Configuration, db2 *keppel.DB, ad2 *test.AuthDriver, sd2 *test.StorageDriver) {
			token := getTokenForSecondary(t, h2, ad2, "repository:test1/foo:pull",
				keppel.CanPullFromAccount)

			if firstPass {
				//do not step the clock in the second pass, otherwise the AssertDBContent
				//will fail on the changed last_pulled_at timestamp
				clock.Step()
			}
			expectManifestExists(t, h2, token, "test1/foo", list.Manifest, "list", nil)

			if strategy == "on_first_use" {
				easypg.AssertDBContent(t, db2.DbMap.Db, "fixtures/imagelistmanifest-replication-001-after-pull-listmanifest.sql")
			}

			if !firstPass {
				//test that this also transferred the referenced manifests eagerly (this
				//part only runs when the primary registry is not reachable)
				expectManifestExists(t, h2, token, "test1/foo", image1.Manifest, "", nil)
				expectManifestExists(t, h2, token, "test1/foo", image2.Manifest, "", nil)
			}
		})
	})
}

func TestReplicationMissingEntities(t *testing.T) {
	testWithPrimary(t, nil, func(h1 http.Handler, cfg1 keppel.Configuration, db1 *keppel.DB, ad1 *test.AuthDriver, sd1 *test.StorageDriver, fd1 *test.FederationDriver, clock *test.Clock) {
		//ensure that the `test1/foo` repo exists upstream; otherwise we'll just get
		//NAME_UNKNOWN
		_, err := keppel.FindOrCreateRepository(db1, "foo", keppel.Account{Name: "test1"})
		if err != nil {
			t.Fatal(err.Error())
		}

		testWithAllReplicaTypes(t, h1, db1, clock, func(strategy string, firstPass bool, h2 http.Handler, cfg2 keppel.Configuration, db2 *keppel.DB, ad2 *test.AuthDriver, sd2 *test.StorageDriver) {
			var (
				expectedStatus        = http.StatusNotFound
				expectedManifestError = keppel.ErrManifestUnknown
			)
			if !firstPass {
				//in the second pass, when the upstream registry is not reachable, we will get network errors instead
				expectedStatus = http.StatusServiceUnavailable
				expectedManifestError = keppel.ErrUnavailable
			}

			//try to pull a manifest by tag that exists neither locally nor upstream
			token := getTokenForSecondary(t, h2, ad2, "repository:test1/foo:pull",
				keppel.CanPullFromAccount)
			assert.HTTPRequest{
				Method:       "GET",
				Path:         "/v2/test1/foo/manifests/thisdoesnotexist",
				Header:       map[string]string{"Authorization": "Bearer " + token},
				ExpectStatus: expectedStatus,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(expectedManifestError),
			}.Check(t, h2)

			//try to pull a manifest by hash that exists neither locally nor upstream
			bogusDigest := "sha256:" + strings.Repeat("0", 64)
			assert.HTTPRequest{
				Method:       "GET",
				Path:         "/v2/test1/foo/manifests/" + bogusDigest,
				Header:       map[string]string{"Authorization": "Bearer " + token},
				ExpectStatus: expectedStatus,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(expectedManifestError),
			}.Check(t, h2)

			//try to pull a blob that exists neither locally nor upstream
			//(this always gives 404 because we don't even try to replicate blobs that
			//are not referenced by a manifest that was already replicated)
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
	testWithPrimary(t, nil, func(h1 http.Handler, cfg1 keppel.Configuration, db1 *keppel.DB, ad1 *test.AuthDriver, sd1 *test.StorageDriver, fd1 *test.FederationDriver, clock *test.Clock) {
		testWithAllReplicaTypes(t, h1, db1, clock, func(strategy string, firstPass bool, h2 http.Handler, cfg2 keppel.Configuration, db2 *keppel.DB, ad2 *test.AuthDriver, sd2 *test.StorageDriver) {
			token := getTokenForSecondary(t, h2, ad2, "repository:test1/foo:pull,push",
				keppel.CanPullFromAccount, keppel.CanPushToAccount)

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
	testWithPrimary(t, nil, func(h1 http.Handler, cfg1 keppel.Configuration, db1 *keppel.DB, ad1 *test.AuthDriver, sd1 *test.StorageDriver, fd1 *test.FederationDriver, clock *test.Clock) {
		//upload image to primary account
		token := getToken(t, h1, ad1, "repository:test1/foo:pull,push",
			keppel.CanPullFromAccount,
			keppel.CanPushToAccount)
		image := test.GenerateImage(test.GenerateExampleLayer(1))
		clock.Step()
		uploadBlob(t, h1, token, "test1/foo", image.Layers[0])
		uploadBlob(t, h1, token, "test1/foo", image.Config)
		uploadManifest(t, h1, token, "test1/foo", image.Manifest, "first")

		//in secondary account...
		testWithAllReplicaTypes(t, h1, db1, clock, func(strategy string, firstPass bool, h2 http.Handler, cfg2 keppel.Configuration, db2 *keppel.DB, ad2 *test.AuthDriver, sd2 *test.StorageDriver) {
			if !firstPass {
				return
			}

			//...lower quotas so that replication will fail
			_, err := db2.Exec(`UPDATE quotas SET manifests = $1`, 0)
			if err != nil {
				t.Fatal(err.Error())
			}

			quotaExceededMessage := test.ErrorCodeWithMessage{
				Code:    keppel.ErrDenied,
				Message: "manifest quota exceeded (quota = 0, usage = 0)",
			}

			token := getTokenForSecondary(t, h2, ad2, "repository:test1/foo:pull",
				keppel.CanPullFromAccount)
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
	testWithPrimary(t, nil, func(h1 http.Handler, cfg1 keppel.Configuration, db1 *keppel.DB, ad1 *test.AuthDriver, sd1 *test.StorageDriver, fd1 *test.FederationDriver, clock *test.Clock) {
		//upload image to primary account
		token := getToken(t, h1, ad1, "repository:test1/foo:pull,push",
			keppel.CanPullFromAccount,
			keppel.CanPushToAccount)
		image := test.GenerateImage(test.GenerateExampleLayer(1))
		clock.Step()
		uploadBlob(t, h1, token, "test1/foo", image.Layers[0])
		uploadBlob(t, h1, token, "test1/foo", image.Config)
		uploadManifest(t, h1, token, "test1/foo", image.Manifest, "first")

		testWithAllReplicaTypes(t, h1, db1, clock, func(strategy string, firstPass bool, h2 http.Handler, cfg2 keppel.Configuration, db2 *keppel.DB, ad2 *test.AuthDriver, sd2 *test.StorageDriver) {
			//in the first pass, just replicate the manifest
			token := getTokenForSecondary(t, h2, ad2, "repository:test1/foo:pull",
				keppel.CanPullFromAccount)
			expectManifestExists(t, h2, token, "test1/foo", image.Manifest, "first", nil)

			//in the second pass, query blobs with HEAD - this should work fine even
			//though the blob contents are not replicated since all necessary metadata
			//can be obtained from the manifest
			for _, blob := range []test.Bytes{image.Config, image.Layers[0]} {
				assert.HTTPRequest{
					Method:       "HEAD",
					Path:         "/v2/test1/foo/blobs/" + blob.Digest.String(),
					Header:       map[string]string{"Authorization": "Bearer " + token},
					ExpectStatus: http.StatusOK,
					ExpectHeader: map[string]string{
						test.VersionHeaderKey:   test.VersionHeaderValue,
						"Content-Length":        strconv.Itoa(len(blob.Contents)),
						"Content-Type":          "application/octet-stream",
						"Docker-Content-Digest": blob.Digest.String(),
					},
				}.Check(t, h2)
			}
		})
	})
}

func TestReplicationForbidAnonymousReplicationFromExternal(t *testing.T) {
	testWithPrimary(t, nil, func(h1 http.Handler, cfg1 keppel.Configuration, db1 *keppel.DB, ad1 *test.AuthDriver, sd1 *test.StorageDriver, fd1 *test.FederationDriver, clock *test.Clock) {
		//upload image to primary account
		token := getToken(t, h1, ad1, "repository:test1/foo:pull,push",
			keppel.CanPullFromAccount,
			keppel.CanPushToAccount)
		image := test.GenerateImage(test.GenerateExampleLayer(1))
		clock.Step()
		uploadBlob(t, h1, token, "test1/foo", image.Layers[0])
		uploadBlob(t, h1, token, "test1/foo", image.Config)
		uploadManifest(t, h1, token, "test1/foo", image.Manifest, "first")
		uploadManifest(t, h1, token, "test1/foo", image.Manifest, "second")

		testWithReplica(t, h1, db1, clock, "from_external_on_first_use", func(firstPass bool, h2 http.Handler, cfg2 keppel.Configuration, db2 *keppel.DB, ad2 *test.AuthDriver, sd2 *test.StorageDriver) {
			//need only one pass for this test
			if !firstPass {
				return
			}

			//make sure that the "test1/foo" repo exists on secondary (otherwise we
			//will get useless NAME_UNKNOWN errors later, not the errors we're interested in)
			token := getTokenForSecondary(t, h2, ad2, "repository:test1/foo:pull",
				keppel.CanPullFromAccount)
			expectManifestExists(t, h2, token, "test1/foo", image.Manifest, "second", nil)

			//enable anonymous pull on the account
			err := db2.Insert(&keppel.RBACPolicy{
				AccountName:        "test1",
				RepositoryPattern:  ".*",
				CanPullAnonymously: true,
			})
			if err != nil {
				t.Fatal(err.Error())
			}

			//get an anonymous token (this is a bit unwieldy because usually all
			//tests work with non-anonymous tokens, so we don't have helper functions
			//for anonymous tokens)
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

			//replicating pull is forbidden with an anonymous token...
			assert.HTTPRequest{
				Method:       "GET",
				Path:         "/v2/test1/foo/manifests/first",
				Header:       map[string]string{"Authorization": "Bearer " + anonToken},
				ExpectStatus: http.StatusForbidden,
				ExpectBody: test.ErrorCodeWithMessage{
					Code:    keppel.ErrDenied,
					Message: "image does not exist here, and anonymous users may not replicate images",
				},
			}.Check(t, h2)

			//...but allowed with a non-anonymous token...
			expectManifestExists(t, h2, token, "test1/foo", image.Manifest, "first", nil)
			//...and once replicated, the anonymous token can pull as well
			expectManifestExists(t, h2, anonToken, "test1/foo", image.Manifest, "first", nil)

		})
	})
}
