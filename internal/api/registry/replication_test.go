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
	"strings"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

//NOTE: ROFU = ReplicationOnFirstUse

func TestROFUSimpleImage(t *testing.T) {
	h1, _, db1, ad1, _, clock := setup(t)

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
	testWithReplica(t, h1, db1, clock, func(firstPass bool, h2 http.Handler, cfg2 keppel.Configuration, db2 *keppel.DB, ad2 *test.AuthDriver, sd2 *test.StorageDriver) {
		token := getTokenForSecondary(t, h2, ad2, "repository:test1/foo:pull",
			keppel.CanPullFromAccount)

		clock.Step()
		expectManifestExists(t, h2, token, "test1/foo", image.Manifest, image.Manifest.Digest.String())

		if firstPass {
			easypg.AssertDBContent(t, db2.DbMap.Db, "fixtures/imagemanifest-replication-001-after-pull-manifest.sql")
		}

		clock.Step()
		expectBlobExists(t, h2, token, "test1/foo", image.Config)
		expectBlobExists(t, h2, token, "test1/foo", image.Layers[0])

		if firstPass {
			easypg.AssertDBContent(t, db2.DbMap.Db, "fixtures/imagemanifest-replication-002-after-pull-blobs.sql")
		}
	})

	//test pull by tag in secondary account
	testWithReplica(t, h1, db1, clock, func(firstPass bool, h2 http.Handler, cfg2 keppel.Configuration, db2 *keppel.DB, ad2 *test.AuthDriver, sd2 *test.StorageDriver) {
		token := getTokenForSecondary(t, h2, ad2, "repository:test1/foo:pull",
			keppel.CanPullFromAccount)
		expectManifestExists(t, h2, token, "test1/foo", image.Manifest, "first")
		expectBlobExists(t, h2, token, "test1/foo", image.Config)
		expectBlobExists(t, h2, token, "test1/foo", image.Layers[0])
	})
}

func TestROFUImageList(t *testing.T) {
	h1, _, db1, ad1, _, clock := setup(t)

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
	testWithReplica(t, h1, db1, clock, func(firstPass bool, h2 http.Handler, cfg2 keppel.Configuration, db2 *keppel.DB, ad2 *test.AuthDriver, sd2 *test.StorageDriver) {
		token := getTokenForSecondary(t, h2, ad2, "repository:test1/foo:pull",
			keppel.CanPullFromAccount)

		clock.Step()
		expectManifestExists(t, h2, token, "test1/foo", list.Manifest, "list")

		easypg.AssertDBContent(t, db2.DbMap.Db, "fixtures/imagelistmanifest-replication-001-after-pull-listmanifest.sql")

		if !firstPass {
			//test that this also transferred the referenced manifests eagerly (this
			//part only runs when the primary registry is not reachable)
			expectManifestExists(t, h2, token, "test1/foo", image1.Manifest, "")
			expectManifestExists(t, h2, token, "test1/foo", image2.Manifest, "")
		}
	})
}

func TestROFUMissingEntities(t *testing.T) {
	h1, _, db1, _, _, clock := setup(t)

	//ensure that the `test1/foo` repo exists upstream; otherwise we'll just get
	//NAME_UNKNOWN
	_, err := keppel.FindOrCreateRepository(db1, "foo", keppel.Account{Name: "test1"})
	if err != nil {
		t.Fatal(err.Error())
	}

	testWithReplica(t, h1, db1, clock, func(firstPass bool, h2 http.Handler, cfg2 keppel.Configuration, db2 *keppel.DB, ad2 *test.AuthDriver, sd2 *test.StorageDriver) {
		var (
			expectedStatus        = http.StatusNotFound
			expectedManifestError = keppel.ErrManifestUnknown
		)
		if !firstPass {
			//in the second pass, when the upstream registry is not reachable, we will get network errors instead
			//(TODO this might warrant its own distinct error code and a 502 status instead)
			expectedStatus = http.StatusInternalServerError
			expectedManifestError = keppel.ErrUnknown
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
}

func TestROFUForbidDirectUpload(t *testing.T) {
	h1, _, db1, _, _, clock := setup(t)
	testWithReplica(t, h1, db1, clock, func(firstPass bool, h2 http.Handler, cfg2 keppel.Configuration, db2 *keppel.DB, ad2 *test.AuthDriver, sd2 *test.StorageDriver) {
		token := getTokenForSecondary(t, h2, ad2, "repository:test1/foo:pull,push",
			keppel.CanPullFromAccount, keppel.CanPushToAccount)

		deniedMessage := test.ErrorCodeWithMessage{
			Code:    keppel.ErrUnsupported,
			Message: "cannot push into replica account (push to registry.example.org/test1/foo instead!)",
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
}

//TODO TestROFUManifestQuotaExceeded
