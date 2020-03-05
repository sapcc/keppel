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
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestImageManifestLifecycle(t *testing.T) {
	image := test.GenerateImage(nil) //nil = no layers, just one blob (image config)

	for _, ref := range []string{"latest", image.Manifest.Digest.String()} {
		h, _, db, ad, sd, clock := setup(t)
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
				ExpectBody:   test.ErrorCode(keppel.ErrNameUnknown),
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
				ExpectBody:   test.ErrorCode(keppel.ErrManifestUnknown),
			}.Check(t, h)
		}

		//PUT failure case: cannot push with read-only token
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/v2/test1/foo/manifests/" + ref,
			Header: map[string]string{
				"Authorization": "Bearer " + readOnlyToken,
				"Content-Type":  image.MediaType,
			},
			Body:         assert.ByteData(image.Manifest.Contents),
			ExpectStatus: http.StatusForbidden,
			ExpectBody:   test.ErrorCode(keppel.ErrDenied),
		}.Check(t, h)

		//PUT failure case: malformed manifest
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/v2/test1/foo/manifests/" + ref,
			Header: map[string]string{
				"Authorization": "Bearer " + token,
				"Content-Type":  image.MediaType,
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
				"Content-Type":  image.MediaType,
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
				"Content-Type":  image.MediaType,
			},
			Body:         assert.ByteData(image.Manifest.Contents),
			ExpectStatus: http.StatusNotFound,
			ExpectBody:   test.ErrorCode(keppel.ErrManifestBlobUnknown),
		}.Check(t, h)

		//failed requests should not retain anything in the storage
		expectStorageEmpty(t, sd, db)

		//PUT failure case: cannot upload manifest if referenced blob is uploaded, but
		//in the wrong repo
		uploadBlob(t, h, otherRepoToken, "test1/bar", image.Config)
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/v2/test1/foo/manifests/" + ref,
			Header: map[string]string{
				"Authorization": "Bearer " + token,
				"Content-Type":  image.MediaType,
			},
			Body:         assert.ByteData(image.Manifest.Contents),
			ExpectStatus: http.StatusNotFound,
			ExpectBody:   test.ErrorCode(keppel.ErrManifestBlobUnknown),
		}.Check(t, h)

		//PUT success case: upload manifest (and also the blob referenced by it);
		//each PUT is executed twice to test idempotency
		clock.Step()
		easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/imagemanifest-001-before-upload-blob.sql")

		uploadBlob(t, h, token, "test1/foo", image.Config)
		uploadBlob(t, h, token, "test1/foo", image.Config)
		clock.Step()
		easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/imagemanifest-002-after-upload-blob.sql")

		uploadManifest(t, h, token, "test1/foo", image.Manifest, image.MediaType, ref)
		uploadManifest(t, h, token, "test1/foo", image.Manifest, image.MediaType, ref)
		clock.Step()
		if ref == "latest" {
			easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/imagemanifest-003-after-upload-manifest-by-tag.sql")
		} else {
			easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/imagemanifest-003-after-upload-manifest-by-digest.sql")
		}

		//check GET/HEAD: manifest should now be available under the reference
		//where it was pushed to...
		expectManifestExists(t, h, readOnlyToken, "test1/foo", image.Manifest, image.MediaType, ref)
		//...and under its digest
		expectManifestExists(t, h, readOnlyToken, "test1/foo", image.Manifest, image.MediaType, image.Manifest.Digest.String())

		//GET failure case: wrong scope
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
			Header:       map[string]string{"Authorization": "Bearer " + otherRepoToken},
			ExpectStatus: http.StatusForbidden,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   test.ErrorCode(keppel.ErrDenied),
		}.Check(t, h)
		//^ NOTE: docker-registry sends UNAUTHORIZED (401) instead of DENIED (403)
		//        here, but 403 is more correct.

		//DELETE failure case: no delete permission
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
			Header:       map[string]string{"Authorization": "Bearer " + token},
			ExpectStatus: http.StatusForbidden,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   test.ErrorCode(keppel.ErrDenied),
		}.Check(t, h)

		//DELETE failure case: cannot delete by tag
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/v2/test1/foo/manifests/latest",
			Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
			ExpectStatus: http.StatusBadRequest,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   test.ErrorCode(keppel.ErrDigestInvalid),
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

		//DELETE success case
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/v2/test1/foo/manifests/" + image.Manifest.Digest.String(),
			Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
			ExpectStatus: http.StatusAccepted,
			ExpectHeader: test.VersionHeader,
		}.Check(t, h)
		clock.Step()
		easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/imagemanifest-002-after-upload-blob.sql")
	}
}

func TestManifestRequiredLabels(t *testing.T) {
	h, _, db, ad, _, _ := setup(t)
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
}
