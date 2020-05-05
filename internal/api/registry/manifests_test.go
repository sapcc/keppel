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
	image := test.GenerateImage( /* no layers */ )

	for _, ref := range []string{"latest", image.Manifest.Digest.String()} {
		h, _, db, ad, sd, clock := setup(t, nil)
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
				"Content-Type":  image.Manifest.MediaType,
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

		//check GET/HEAD: manifest should now be available under the reference
		//where it was pushed to...
		expectManifestExists(t, h, readOnlyToken, "test1/foo", image.Manifest, ref)
		//...and under its digest
		expectManifestExists(t, h, readOnlyToken, "test1/foo", image.Manifest, image.Manifest.Digest.String())

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

func TestImageListManifestLifecycle(t *testing.T) {
	//This test builds on TestImageManifestLifecycle and provides test coverage
	//for the parts of the manifest push workflow that check manifest-manifest
	//references. (We don't have those in plain images, only in image lists.)
	h, _, db, ad, _, clock := setup(t, nil)
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
}

func TestManifestQuotaExceeded(t *testing.T) {
	h, _, db, ad, _, _ := setup(t, nil)
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
}

func TestManifestRequiredLabels(t *testing.T) {
	h, _, db, ad, _, _ := setup(t, nil)
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

func TestImageManifestWrongBlobSize(t *testing.T) {
	h, _, _, ad, _, _ := setup(t, nil)
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
}
