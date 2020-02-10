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
	"io/ioutil"
	"net/http"
	"strconv"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestManifestRequiredLabels(t *testing.T) {
	h, _, db, ad, _, _ := setup(t)
	token := getToken(t, h, ad, "repository:test1/foo:pull,push",
		keppel.CanPullFromAccount,
		keppel.CanPushToAccount)

	//setup test data: image config with labels "foo" and "bar"
	blobContents, err := ioutil.ReadFile("fixtures/example-docker-image-config-with-labels.json")
	if err != nil {
		t.Fatal(err.Error())
	}
	blobDigest := "sha256:" + sha256Of(blobContents)

	//setup test data: manifest referencing that image config
	manifestData := assert.JSONObject{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.docker.distribution.manifest.v2+json",
		"config": assert.JSONObject{
			"mediaType": "application/vnd.docker.container.image.v1+json",
			"size":      len(blobContents),
			"digest":    blobDigest,
		},
		"layers": []assert.JSONObject{},
	}

	//upload the config blob
	assert.HTTPRequest{
		Method: "POST",
		Path:   "/v2/test1/foo/blobs/uploads/?digest=" + blobDigest,
		Header: map[string]string{
			"Authorization":  "Bearer " + token,
			"Content-Length": strconv.Itoa(len(blobContents)),
			"Content-Type":   "application/octet-stream",
		},
		Body:         assert.ByteData(blobContents),
		ExpectStatus: http.StatusCreated,
	}.Check(t, h)

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
