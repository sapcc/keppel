/******************************************************************************
*
*  Copyright 2019 SAP SE
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
	"bytes"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

//TODO unbundle these testcases, now that we don't have to start actual
//docker-registry processes anymore
func TestProxyAPI(t *testing.T) {
	h, _, db, ad, _, clock := setup(t)

	_, err := keppel.FindOrCreateRepository(db, "foo", keppel.Account{Name: "test1"})
	if err != nil {
		t.Fatal(err.Error())
	}

	clock.Step()
	testVersionCheckEndpoint(t, h, ad)
	clock.Step()
	clock.Step()
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/001-before-push.sql")
	firstBlobDigest := testPushAndPull(t, h, ad, db,
		"fixtures/example-docker-image-config.json",
		"fixtures/002-after-push.sql",
	)
	clock.Step()
	testPushAndPull(t, h, ad, db,
		"fixtures/example-docker-image-config2.json",
		"fixtures/003-after-second-push.sql",
	)
	clock.Step()
	testReplicationOnFirstUse(t, h, db,
		//the first manifest, which is not referenced by a tag
		"sha256:86fa8722ca7f27e97e1bc5060c3f6720bf43840f143f813fcbe48ed4cbeebb90",
		//the blob contained in that manifest
		firstBlobDigest,
		//the second manifest, which is referenced by a tag
		"sha256:65147aad93781ff7377b8fb81dab153bd58ffe05b5dc00b67b3035fa9420d2de",
		"latest", //the tag
	)
}

func testVersionCheckEndpoint(t *testing.T, h http.Handler, ad keppel.AuthDriver) {
	//without token, expect auth challenge
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/",
		Header:       addHeadersForCorrectAuthChallenge(nil),
		ExpectStatus: http.StatusUnauthorized,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Www-Authenticate":    `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org"`,
		},
		ExpectBody: assert.JSONObject{
			"errors": []assert.JSONObject{{
				"code":    keppel.ErrUnauthorized,
				"detail":  nil,
				"message": "no bearer token found in request headers",
			}},
		},
	}.Check(t, h)

	//with token, expect status code 200
	token := getToken(t, h, ad, "" /* , no permissions */)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: test.VersionHeader,
	}.Check(t, h)

	if t.Failed() {
		t.FailNow()
	}
}

func testPushAndPull(t *testing.T, h http.Handler, ad keppel.AuthDriver, db *keppel.DB, imageConfigJSON, dbContentsAfterManifestPush string) string {
	//This tests pushing a minimal image without any layers, so we only upload one object (the config JSON) and create a manifest.
	token := getToken(t, h, ad, "repository:test1/foo:pull,push",
		keppel.CanPullFromAccount,
		keppel.CanPushToAccount)

	//upload config JSON
	bodyBytes, err := ioutil.ReadFile(imageConfigJSON)
	if err != nil {
		t.Fatal(err.Error())
	}
	bodyBytes = bytes.TrimSpace(bodyBytes)
	sha256HashStr := test.UploadBlobToRegistry(t, h, "test1/foo", token, bodyBytes)

	//create manifest (this request is executed twice to test idempotency)
	manifestData := assert.JSONObject{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.docker.distribution.manifest.v2+json",
		"config": assert.JSONObject{
			"mediaType": "application/vnd.docker.container.image.v1+json",
			"size":      len(bodyBytes),
			"digest":    "sha256:" + sha256HashStr,
		},
		"layers": []assert.JSONObject{},
	}
	for range []int{1, 2} {
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
		if t.Failed() {
			t.FailNow()
		}
		//check that repo/manifest/tag was created correctly in our DB
		easypg.AssertDBContent(t, db.DbMap.Db, dbContentsAfterManifestPush)
	}

	//verify that "latest" now appears in tag list
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/tags/list",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: test.VersionHeader,
		ExpectBody: assert.JSONObject{
			"name": "test1/foo",
			"tags": []string{"latest"},
		},
	}.Check(t, h)

	//pull manifest using a read-only token
	token = getToken(t, h, ad, "repository:test1/foo:pull",
		keppel.CanPullFromAccount)
	assert.HTTPRequest{
		Method: "GET",
		Path:   "/v2/test1/foo/manifests/latest",
		Header: map[string]string{
			"Accept":        "application/vnd.docker.distribution.manifest.v2+json",
			"Authorization": "Bearer " + token,
		},
		ExpectStatus: http.StatusOK,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Content-Type":        "application/vnd.docker.distribution.manifest.v2+json",
		},
		ExpectBody: manifestData,
	}.Check(t, h)

	//pull config layer
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/blobs/sha256:" + sha256HashStr,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Content-Type":        "application/octet-stream",
		},
		ExpectBody: assert.JSONFixtureFile(imageConfigJSON),
	}.Check(t, h)

	return "sha256:" + sha256HashStr
}
