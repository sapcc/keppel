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
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestBlobMonolithicUpload(t *testing.T) {
	h, _, db, ad, sd, _ := setup(t)
	readOnlyToken := getToken(t, h, ad, "repository:test1/foo:pull,push",
		keppel.CanPullFromAccount)
	token := getToken(t, h, ad, "repository:test1/foo:pull,push",
		keppel.CanPullFromAccount,
		keppel.CanPushToAccount)

	blobContents := []byte("just some random data")
	blobDigest := "sha256:" + sha256Of(blobContents)

	//test failure cases: token does not have push access
	assert.HTTPRequest{
		Method: "POST",
		Path:   "/v2/test1/foo/blobs/uploads/?digest=" + blobDigest,
		Header: map[string]string{
			"Authorization":  "Bearer " + readOnlyToken,
			"Content-Length": strconv.Itoa(len(blobContents)),
			"Content-Type":   "application/octet-stream",
		},
		Body:         test.ByteData(blobContents),
		ExpectStatus: http.StatusForbidden,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrDenied),
	}.Check(t, h)

	//test failure cases: digest is wrong
	for _, wrongDigest := range []string{"wrong", "sha256:" + sha256Of([]byte("something else"))} {
		assert.HTTPRequest{
			Method: "POST",
			Path:   "/v2/test1/foo/blobs/uploads/?digest=" + wrongDigest,
			Header: map[string]string{
				"Authorization":  "Bearer " + token,
				"Content-Length": strconv.Itoa(len(blobContents)),
				"Content-Type":   "application/octet-stream",
			},
			Body:         test.ByteData(blobContents),
			ExpectStatus: http.StatusBadRequest,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   test.ErrorCode(keppel.ErrDigestInvalid),
		}.Check(t, h)
	}

	//test failure cases: Content-Length is wrong
	for _, wrongLength := range []string{"", "wrong", "1337"} {
		assert.HTTPRequest{
			Method: "POST",
			Path:   "/v2/test1/foo/blobs/uploads/?digest=" + blobDigest,
			Header: map[string]string{
				"Authorization":  "Bearer " + token,
				"Content-Length": wrongLength,
				"Content-Type":   "application/octet-stream",
			},
			Body:         test.ByteData(blobContents),
			ExpectStatus: http.StatusBadRequest,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   test.ErrorCode(keppel.ErrSizeInvalid),
		}.Check(t, h)
	}

	//failed requests should not retain anything in the storage
	expectStorageEmpty(t, sd, db)

	//test success case
	assert.HTTPRequest{
		Method: "POST",
		Path:   "/v2/test1/foo/blobs/uploads/?digest=" + blobDigest,
		Header: map[string]string{
			"Authorization":  "Bearer " + token,
			"Content-Length": strconv.Itoa(len(blobContents)),
			"Content-Type":   "application/octet-stream",
		},
		Body:         test.ByteData(blobContents),
		ExpectStatus: http.StatusCreated,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Content-Length":      "0",
			"Location":            "/v2/test1/foo/blobs/" + blobDigest,
		},
	}.Check(t, h)

	//validate that the blob was stored at the specified location
	expectBlobContents(t, h, token, blobDigest, blobContents)
}

func expectBlobContents(t *testing.T, h http.Handler, token, blobDigest string, blobContents []byte) {
	for _, method := range []string{"GET", "HEAD"} {
		respBody := blobContents
		if method == "HEAD" {
			respBody = nil
		}
		assert.HTTPRequest{
			Method:       method,
			Path:         "/v2/test1/foo/blobs/" + blobDigest,
			Header:       map[string]string{"Authorization": "Bearer " + token},
			ExpectStatus: http.StatusOK,
			ExpectHeader: map[string]string{
				test.VersionHeaderKey:   test.VersionHeaderValue,
				"Content-Length":        strconv.Itoa(len(blobContents)),
				"Content-Type":          "application/octet-stream",
				"Docker-Content-Digest": blobDigest,
			},
			ExpectBody: test.ByteData(respBody),
		}.Check(t, h)
	}
}

func expectStorageEmpty(t *testing.T, sd *test.StorageDriver, db *keppel.DB) {
	t.Helper()
	//test that no blobs were yet commited to the DB...
	count, err := db.SelectInt(`SELECT COUNT(*) FROM blobs`)
	if err != nil {
		t.Fatal(err.Error())
	}
	if count > 0 {
		t.Errorf("expected 0 blobs in the DB, but found %d blobs", count)
	}

	//...nor to the storage
	if sd.BlobCount() > 0 {
		t.Errorf("expected 0 blobs in the storage, but found %d blobs", sd.BlobCount())
	}

	//also there should be no unfinished uploads
	count, err = db.SelectInt(`SELECT COUNT(*) FROM uploads`)
	if err != nil {
		t.Fatal(err.Error())
	}
	if count > 0 {
		t.Errorf("expected 0 uploads in the DB, but found %d uploads", count)
	}
}

func getBlobUploadURL(t *testing.T, h http.Handler, token string) string {
	resp, _ := assert.HTTPRequest{
		Method:       "POST",
		Path:         "/v2/test1/foo/blobs/uploads/",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusAccepted,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Content-Length":      "0",
			"Range":               "0-0",
		},
	}.Check(t, h)
	return resp.Header.Get("Location")
}

func TestBlobStreamedUpload(t *testing.T) {
	h, _, db, ad, sd, _ := setup(t)
	readOnlyToken := getToken(t, h, ad, "repository:test1/foo:pull,push",
		keppel.CanPullFromAccount)
	token := getToken(t, h, ad, "repository:test1/foo:pull,push",
		keppel.CanPullFromAccount,
		keppel.CanPushToAccount)

	blobContents := []byte("just some random data")
	blobDigest := "sha256:" + sha256Of(blobContents)

	//shorthand for a common header structure that appears in many requests below
	tokenAndContentType := map[string]string{
		"Authorization": "Bearer " + token,
		"Content-Type":  "application/octet-stream",
	}

	//test failure cases during POST: token does not have push access
	assert.HTTPRequest{
		Method: "POST",
		Path:   "/v2/test1/foo/blobs/uploads/",
		Header: map[string]string{
			"Authorization":  "Bearer " + readOnlyToken,
			"Content-Length": strconv.Itoa(len(blobContents)),
			"Content-Type":   "application/octet-stream",
		},
		Body:         test.ByteData(blobContents),
		ExpectStatus: http.StatusForbidden,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrDenied),
	}.Check(t, h)

	//test failure cases during PATCH: bogus session ID
	assert.HTTPRequest{
		Method:       "PATCH",
		Path:         "/v2/test1/foo/blobs/uploads/b9ef33aa-7e2a-4fc8-8083-6b00601dab98", //bogus session ID
		Header:       tokenAndContentType,
		Body:         test.ByteData(blobContents),
		ExpectStatus: http.StatusNotFound,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrBlobUploadUnknown),
	}.Check(t, h)

	//test failure cases during PATCH: unexpected session state (the first PATCH
	//request should not contain session state)
	assert.HTTPRequest{
		Method:       "PATCH",
		Path:         keppel.AppendQuery(getBlobUploadURL(t, h, token), url.Values{"state": {"unexpected"}}),
		Header:       tokenAndContentType,
		Body:         test.ByteData(blobContents),
		ExpectStatus: http.StatusBadRequest,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrBlobUploadInvalid),
	}.Check(t, h)

	//test failure cases during PATCH: malformed session state (this requires a
	//successful PATCH first, otherwise the API would not expect to find session
	//state in our request)
	uploadURL := getBlobUploadURL(t, h, token)
	assert.HTTPRequest{
		Method:       "PATCH",
		Path:         uploadURL,
		Header:       tokenAndContentType,
		Body:         test.ByteData(blobContents),
		ExpectStatus: http.StatusAccepted,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "PATCH",
		Path:         keppel.AppendQuery(uploadURL, url.Values{"state": {"unexpected"}}),
		Header:       tokenAndContentType,
		Body:         test.ByteData(blobContents),
		ExpectStatus: http.StatusBadRequest,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrBlobUploadInvalid),
	}.Check(t, h)

	//helper function: upload all the blob contents with a single PATCH, so that
	//we can test error cases for PUT
	getUploadURLForPUT := func() string {
		resp, _ := assert.HTTPRequest{
			Method:       "PATCH",
			Path:         getBlobUploadURL(t, h, token),
			Header:       tokenAndContentType,
			Body:         test.ByteData(blobContents),
			ExpectStatus: http.StatusAccepted,
		}.Check(t, h)
		return resp.Header.Get("Location")
	}

	//test failure cases during PUT: digest is missing or wrong
	for _, wrongDigest := range []string{"", "wrong", "sha256:" + sha256Of([]byte("something else"))} {
		assert.HTTPRequest{
			Method:       "PUT",
			Path:         keppel.AppendQuery(getUploadURLForPUT(), url.Values{"digest": {wrongDigest}}),
			Header:       map[string]string{"Authorization": "Bearer " + token},
			ExpectStatus: http.StatusBadRequest,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   test.ErrorCode(keppel.ErrDigestInvalid),
		}.Check(t, h)
	}

	//failed requests should not retain anything in the storage
	expectStorageEmpty(t, sd, db)

	//test success case (with multiple chunks!)
	uploadURL = getBlobUploadURL(t, h, token)
	progress := 0
	for _, chunk := range bytes.SplitAfter(blobContents, []byte(" ")) {
		progress += len(chunk)

		if progress == len(blobContents) {
			//send the last chunk with the final PUT request
			assert.HTTPRequest{
				Method: "PUT",
				Path:   keppel.AppendQuery(uploadURL, url.Values{"digest": {blobDigest}}),
				Header: map[string]string{
					"Authorization":  "Bearer " + token,
					"Content-Length": strconv.Itoa(len(chunk)),
					"Content-Type":   "application/octet-stream",
				},
				Body:         test.ByteData(chunk),
				ExpectStatus: http.StatusCreated,
				ExpectHeader: map[string]string{
					test.VersionHeaderKey: test.VersionHeaderValue,
					"Content-Length":      "0",
					"Location":            "/v2/test1/foo/blobs/" + blobDigest,
				},
			}.Check(t, h)
		} else {
			resp, _ := assert.HTTPRequest{
				Method:       "PATCH",
				Path:         uploadURL,
				Header:       tokenAndContentType,
				Body:         test.ByteData(chunk),
				ExpectStatus: http.StatusAccepted,
				ExpectHeader: map[string]string{
					test.VersionHeaderKey: test.VersionHeaderValue,
					"Content-Length":      "0",
					"Range":               fmt.Sprintf("0-%d", progress),
				},
			}.Check(t, h)
			uploadURL = resp.Header.Get("Location")
		}
	}

	//validate that the blob was stored at the specified location
	expectBlobContents(t, h, token, blobDigest, blobContents)
}
