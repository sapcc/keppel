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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/keppel/internal/keppel"
)

const (
	//VersionHeaderKey is the standard version header name included in all Registry v2 API responses.
	VersionHeaderKey = "Docker-Distribution-Api-Version"
	//VersionHeaderValue is the standard version header value included in all Registry v2 API responses.
	VersionHeaderValue = "registry/2.0"
)

//VersionHeader is the standard version header included in all Registry v2 API responses.
var VersionHeader = map[string]string{VersionHeaderKey: VersionHeaderValue}

//UploadBlobToRegistry uploads a blob to the registry.
//
//`h` must be a handler serving the Registry V2 API.
//`repo` is the target repository name (including the account name).
//`token` is a Bearer token capable of pushing into that repo.
//`contentBytes` is the contents of the blob.
func UploadBlobToRegistry(t *testing.T, h http.Handler, repo, token string, contentBytes []byte) (digest string) {
	t.Helper()

	//initiate upload for the image config
	resp, _ := assert.HTTPRequest{
		Method:       "POST",
		Path:         fmt.Sprintf("/v2/%s/blobs/uploads/", repo),
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusAccepted,
		ExpectHeader: VersionHeader,
	}.Check(t, h)
	if t.Failed() {
		t.FailNow()
	}
	uploadPath := resp.Header.Get("Location")

	//send config data
	resp, _ = assert.HTTPRequest{
		Method: "PATCH",
		Path:   uploadPath,
		Header: map[string]string{
			"Authorization":  "Bearer " + token,
			"Content-Length": fmt.Sprintf("%d", len(contentBytes)),
			"Content-Range":  fmt.Sprintf("bytes=0-%d", len(contentBytes)),
			"Content-Type":   "application/octet-stream",
		},
		Body:         byteData(contentBytes),
		ExpectStatus: http.StatusAccepted,
		ExpectHeader: VersionHeader,
	}.Check(t, h)
	if t.Failed() {
		t.FailNow()
	}
	uploadPath = resp.Header.Get("Location")

	//finish config upload
	query := url.Values{}
	sha256Hash := sha256.Sum256(contentBytes)
	sha256HashStr := hex.EncodeToString(sha256Hash[:])
	query.Set("digest", "sha256:"+sha256HashStr)
	resp, _ = assert.HTTPRequest{
		Method: "PUT",
		Path:   keppel.AppendQuery(uploadPath, query),
		Header: map[string]string{
			"Authorization":  "Bearer " + token,
			"Content-Length": "0",
		},
		ExpectStatus: http.StatusCreated,
		ExpectHeader: VersionHeader,
	}.Check(t, h)
	if t.Failed() {
		t.FailNow()
	}
	layerPath := resp.Header.Get("Location")

	//validate config upload
	resp, _ = assert.HTTPRequest{
		Method:       "HEAD",
		Path:         layerPath,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: VersionHeader,
	}.Check(t, h)
	if t.Failed() {
		t.FailNow()
	}
	assert.DeepEqual(t, "layer Content-Length",
		resp.Header.Get("Content-Length"),
		strconv.FormatUint(uint64(len(contentBytes)), 10),
	)

	return sha256HashStr
}

type byteData []byte

func (b byteData) GetRequestBody() (io.Reader, error) {
	return bytes.NewReader([]byte(b)), nil
}
