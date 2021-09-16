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

package test

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/keppel/internal/keppel"
)

const (
	//VersionHeaderKey is the standard version header name included in all
	//Registry v2 API responses.
	VersionHeaderKey = "Docker-Distribution-Api-Version"
	//VersionHeaderValue is the standard version header value included in all
	//Registry v2 API responses.
	VersionHeaderValue = "registry/2.0"
)

//VersionHeader is the standard version header included in all Registry v2 API
//responses.
var VersionHeader = map[string]string{VersionHeaderKey: VersionHeaderValue}

//MustUpload uploads the blob via the Registry V2 API.
//
//`h` must serve the Registry V2 API.
//`token` must be a Bearer token capable of pushing into the specified repo.
func (b Bytes) MustUpload(t *testing.T, h http.Handler, token string, repo keppel.Repository) {
	//create blob with a monolithic upload
	assert.HTTPRequest{
		Method: "POST",
		Path:   fmt.Sprintf("/v2/%s/blobs/uploads/?digest=%s", repo.FullName(), b.Digest),
		Header: map[string]string{
			"Authorization":  "Bearer " + token,
			"Content-Length": strconv.Itoa(len(b.Contents)),
			"Content-Type":   b.MediaType,
		},
		Body:         assert.ByteData(b.Contents),
		ExpectStatus: http.StatusCreated,
	}.Check(t, h)
	if t.Failed() {
		t.FailNow()
	}

	//validate blob
	assert.HTTPRequest{
		Method:       "HEAD",
		Path:         fmt.Sprintf("/v2/%s/blobs/%s", repo.FullName(), b.Digest),
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: map[string]string{
			VersionHeaderKey: VersionHeaderValue,
			"Content-Length": strconv.Itoa(len(b.Contents)),
		},
	}.Check(t, h)
	if t.Failed() {
		t.FailNow()
	}
}

var checkBlobExistsQuery = keppel.SimplifyWhitespaceInSQL(`
	SELECT COUNT(*) FROM blobs WHERE account_name = $1 AND digest = $2
`)

//MustUpload uploads the image via the Registry V2 API. This also
//uploads all referenced blobs that do not exist in the DB yet.
//
//`h` must serve the Registry V2 API.
//`token` must be a Bearer token capable of pushing into the specified repo.
//`tagName` may be empty if the image is to be uploaded without tagging.
func (i Image) MustUpload(t *testing.T, h http.Handler, db *keppel.DB, token string, repo keppel.Repository, tagName string) {
	//upload missing blobs
	for _, blob := range append(i.Layers, i.Config) {
		count, err := db.SelectInt(checkBlobExistsQuery, repo.AccountName, blob.Digest.String())
		if err != nil {
			t.Fatal(err.Error())
		}
		if count == 0 {
			blob.MustUpload(t, h, token, repo)
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	//upload manifest
	ref := i.DigestRef()
	if tagName != "" {
		ref = keppel.ManifestReference{Tag: tagName}
	}
	urlPath := fmt.Sprintf("/v2/%s/manifests/%s", repo.FullName(), ref)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   urlPath,
		Header: map[string]string{
			"Authorization": "Bearer " + token,
			"Content-Type":  i.Manifest.MediaType,
		},
		Body:         assert.ByteData(i.Manifest.Contents),
		ExpectStatus: http.StatusCreated,
	}.Check(t, h)
	if t.Failed() {
		t.FailNow()
	}

	//check uploaded manifest
	assert.HTTPRequest{
		Method:       "HEAD",
		Path:         urlPath,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: map[string]string{
			VersionHeaderKey: VersionHeaderValue,
			"Content-Length": strconv.Itoa(len(i.Manifest.Contents)),
		},
	}.Check(t, h)
	if t.Failed() {
		t.FailNow()
	}
}

var checkManifestExistsQuery = keppel.SimplifyWhitespaceInSQL(`
	SELECT COUNT(*) FROM manifests m
	  JOIN repos r ON m.repo_id = r.id
	 WHERE r.account_name = $1 AND r.name = $2 AND m.digest = $3
`)

//MustUpload uploads the image list via the Registry V2 API. This
//also uploads all referenced images that do not exist in the DB yet.
//
//`h` must serve the Registry V2 API.
//`token` must be a Bearer token capable of pushing into the specified repo.
//`tagName` may be empty if the image is to be uploaded without tagging.
func (l ImageList) MustUpload(t *testing.T, h http.Handler, db *keppel.DB, token string, repo keppel.Repository, tagName string) {
	//upload missing images
	for _, image := range l.Images {
		count, err := db.SelectInt(checkManifestExistsQuery, repo.AccountName, repo.Name, image.Manifest.Digest.String())
		if err != nil {
			t.Fatal(err.Error())
		}
		if count == 0 {
			image.MustUpload(t, h, db, token, repo, "")
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	//upload manifest
	ref := l.DigestRef()
	if tagName != "" {
		ref = keppel.ManifestReference{Tag: tagName}
	}
	urlPath := fmt.Sprintf("/v2/%s/manifests/%s", repo.FullName(), ref)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   urlPath,
		Header: map[string]string{
			"Authorization": "Bearer " + token,
			"Content-Type":  l.Manifest.MediaType,
		},
		Body:         assert.ByteData(l.Manifest.Contents),
		ExpectStatus: http.StatusCreated,
	}.Check(t, h)
	if t.Failed() {
		t.FailNow()
	}

	//check uploaded manifest
	assert.HTTPRequest{
		Method:       "HEAD",
		Path:         urlPath,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: map[string]string{
			VersionHeaderKey: VersionHeaderValue,
			"Content-Length": strconv.Itoa(len(l.Manifest.Contents)),
		},
	}.Check(t, h)
	if t.Failed() {
		t.FailNow()
	}
}
