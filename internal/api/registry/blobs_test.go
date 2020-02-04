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
		Body:         assert.ByteData(blobContents),
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
			Body:         assert.ByteData(blobContents),
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
			Body:         assert.ByteData(blobContents),
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
		Body:         assert.ByteData(blobContents),
		ExpectStatus: http.StatusCreated,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Content-Length":      "0",
			"Location":            "/v2/test1/foo/blobs/" + blobDigest,
		},
	}.Check(t, h)

	//validate that the blob was stored at the specified location
	expectBlobContents(t, h, token, "test1/foo", blobDigest, blobContents)
}

func expectBlobContents(t *testing.T, h http.Handler, token, repoName, blobDigest string, blobContents []byte) {
	for _, method := range []string{"GET", "HEAD"} {
		respBody := blobContents
		if method == "HEAD" {
			respBody = nil
		}
		assert.HTTPRequest{
			Method:       method,
			Path:         "/v2/" + repoName + "/blobs/" + blobDigest,
			Header:       map[string]string{"Authorization": "Bearer " + token},
			ExpectStatus: http.StatusOK,
			ExpectHeader: map[string]string{
				test.VersionHeaderKey:   test.VersionHeaderValue,
				"Content-Length":        strconv.Itoa(len(blobContents)),
				"Content-Type":          "application/octet-stream",
				"Docker-Content-Digest": blobDigest,
			},
			ExpectBody: assert.ByteData(respBody),
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

func getBlobUpload(t *testing.T, h http.Handler, token string) (uploadURL, uploadUUID string) {
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
	return resp.Header.Get("Location"), resp.Header.Get("Blob-Upload-Session-Id")
}

func getBlobUploadURL(t *testing.T, h http.Handler, token string) string {
	u, _ := getBlobUpload(t, h, token)
	return u
}

func TestBlobStreamedAndChunkedUpload(t *testing.T) {
	//run everything in this testcase once for streamed upload and once for chunked upload
	for _, isChunked := range []bool{false, true} {

		h, _, db, ad, sd, _ := setup(t)
		readOnlyToken := getToken(t, h, ad, "repository:test1/foo:pull,push",
			keppel.CanPullFromAccount)
		token := getToken(t, h, ad, "repository:test1/foo:pull,push",
			keppel.CanPullFromAccount,
			keppel.CanPushToAccount)

		blobContents := []byte("just some random data")
		blobDigest := "sha256:" + sha256Of(blobContents)

		//shorthand for a common header structure that appears in many requests below
		getHeadersForPATCH := func(offset, length int) map[string]string {
			hdr := map[string]string{
				"Authorization": "Bearer " + token,
				"Content-Type":  "application/octet-stream",
			}
			//for streamed upload, Content-Range and Content-Length are omitted
			if isChunked {
				hdr["Content-Range"] = fmt.Sprintf("%d-%d", offset, offset+length)
				hdr["Content-Length"] = strconv.Itoa(length)
			}
			return hdr
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
			Body:         assert.ByteData(blobContents),
			ExpectStatus: http.StatusForbidden,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   test.ErrorCode(keppel.ErrDenied),
		}.Check(t, h)

		//test failure cases during PATCH: bogus session ID
		assert.HTTPRequest{
			Method:       "PATCH",
			Path:         "/v2/test1/foo/blobs/uploads/b9ef33aa-7e2a-4fc8-8083-6b00601dab98", //bogus session ID
			Header:       getHeadersForPATCH(0, len(blobContents)),
			Body:         assert.ByteData(blobContents),
			ExpectStatus: http.StatusNotFound,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   test.ErrorCode(keppel.ErrBlobUploadUnknown),
		}.Check(t, h)

		//test failure cases during PATCH: unexpected session state (the first PATCH
		//request should not contain session state)
		assert.HTTPRequest{
			Method:       "PATCH",
			Path:         keppel.AppendQuery(getBlobUploadURL(t, h, token), url.Values{"state": {"unexpected"}}),
			Header:       getHeadersForPATCH(0, len(blobContents)),
			Body:         assert.ByteData(blobContents),
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
			Header:       getHeadersForPATCH(0, len(blobContents)),
			Body:         assert.ByteData(blobContents),
			ExpectStatus: http.StatusAccepted,
		}.Check(t, h)
		assert.HTTPRequest{
			Method:       "PATCH",
			Path:         keppel.AppendQuery(uploadURL, url.Values{"state": {"unexpected"}}),
			Header:       getHeadersForPATCH(len(blobContents), len(blobContents)),
			Body:         assert.ByteData(blobContents),
			ExpectStatus: http.StatusBadRequest,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   test.ErrorCode(keppel.ErrBlobUploadInvalid),
		}.Check(t, h)

		//test failure cases during PATCH: malformed Content-Range and/or
		//Content-Length (only for chunked upload; streamed upload does not have
		//these headers)
		if isChunked {
			testWrongContentRangeAndOrLength := func(contentRange, contentLength string) {
				t.Helper()
				//upload the blob contents in two chunks; we will trigger the error
				//condition in the second PATCH
				chunk1, chunk2 := blobContents[0:10], blobContents[10:15]
				resp, _ := assert.HTTPRequest{
					Method:       "PATCH",
					Path:         getBlobUploadURL(t, h, token),
					Header:       getHeadersForPATCH(0, len(chunk1)),
					Body:         assert.ByteData(chunk1),
					ExpectStatus: http.StatusAccepted,
				}.Check(t, h)
				assert.HTTPRequest{
					Method: "PATCH",
					Path:   resp.Header.Get("Location"),
					Header: map[string]string{
						"Authorization":  "Bearer " + token,
						"Content-Length": contentLength,
						"Content-Range":  contentRange,
						"Content-Type":   "application/octet-stream",
					},
					Body:         assert.ByteData(chunk2),
					ExpectStatus: http.StatusBadRequest,
					ExpectHeader: test.VersionHeader,
					ExpectBody:   test.ErrorCode(keppel.ErrSizeInvalid),
				}.Check(t, h)
			}
			//NOTE: The correct headers would be Content-Range: 10-15 and Content-Length: 5.
			testWrongContentRangeAndOrLength("10-14", "4")                         //both consistently wrong
			testWrongContentRangeAndOrLength("10-15", "6")                         //only Content-Length wrong
			testWrongContentRangeAndOrLength("10-16", "5")                         //only Content-Range wrong
			testWrongContentRangeAndOrLength("8-13", "5")                          //consistent, but wrong offset
			testWrongContentRangeAndOrLength("10-15", "")                          //Content-Length missing
			testWrongContentRangeAndOrLength("10", "5")                            //wrong format for Content-Range
			testWrongContentRangeAndOrLength("10-abc", "5")                        //even wronger format for Content-Range
			testWrongContentRangeAndOrLength("99999999999999999999999999-10", "5") //what are you doing?
			testWrongContentRangeAndOrLength("10-99999999999999999999999999", "5") //omg stop it!
		}

		//test failure cases during PUT: digest is missing or wrong
		for _, wrongDigest := range []string{"", "wrong", "sha256:" + sha256Of([]byte("something else"))} {
			//upload all the blob contents at once (we're only interested in the final PUT)
			resp, _ := assert.HTTPRequest{
				Method:       "PATCH",
				Path:         getBlobUploadURL(t, h, token),
				Header:       getHeadersForPATCH(0, len(blobContents)),
				Body:         assert.ByteData(blobContents),
				ExpectStatus: http.StatusAccepted,
			}.Check(t, h)
			uploadURL := resp.Header.Get("Location")
			assert.HTTPRequest{
				Method:       "PUT",
				Path:         keppel.AppendQuery(uploadURL, url.Values{"digest": {wrongDigest}}),
				Header:       map[string]string{"Authorization": "Bearer " + token},
				ExpectStatus: http.StatusBadRequest,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrDigestInvalid),
			}.Check(t, h)
		}

		//test failure cases during PUT: broken Content-Length on PUT with content
		for _, wrongContentLength := range []string{"", "0", "1024"} {
			//upload the blob contents in two chunks, so that we have a chunk to send with PUT
			chunk1, chunk2 := blobContents[0:10], blobContents[10:]
			resp, _ := assert.HTTPRequest{
				Method:       "PATCH",
				Path:         getBlobUploadURL(t, h, token),
				Header:       getHeadersForPATCH(0, len(chunk1)),
				Body:         assert.ByteData(chunk1),
				ExpectStatus: http.StatusAccepted,
			}.Check(t, h)
			uploadURL := resp.Header.Get("Location")

			//when Content-Length is missing or 0, the request body will just be
			//ignored and the validation will fail later when the digest does not match
			//because of the missing chunk
			expectedError := test.ErrorCode(keppel.ErrSizeInvalid)
			if wrongContentLength == "" || wrongContentLength == "0" {
				expectedError = test.ErrorCode(keppel.ErrDigestInvalid)
			}

			assert.HTTPRequest{
				Method: "PUT",
				Path:   keppel.AppendQuery(uploadURL, url.Values{"digest": {blobDigest}}),
				Header: map[string]string{
					"Authorization":  "Bearer " + token,
					"Content-Length": wrongContentLength,
					"Content-Type":   "application/octet-stream",
				},
				Body:         assert.ByteData(chunk2),
				ExpectStatus: http.StatusBadRequest,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   expectedError,
			}.Check(t, h)

			if t.Failed() {
				t.Fatalf("fails on CL %q", wrongContentLength)
			}
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
					Body:         assert.ByteData(chunk),
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
					Header:       getHeadersForPATCH(progress-len(chunk), len(chunk)),
					Body:         assert.ByteData(chunk),
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
		expectBlobContents(t, h, token, "test1/foo", blobDigest, blobContents)
	}
}

func TestGetBlobUpload(t *testing.T) {
	//NOTE: We only use the read-write token for driving the blob upload through
	//its various stages. All the GET requests use the read-only token to verify
	//that read-only tokens work here.
	h, _, _, ad, _, _ := setup(t)
	readOnlyToken := getToken(t, h, ad, "repository:test1/foo:pull,push",
		keppel.CanPullFromAccount)
	token := getToken(t, h, ad, "repository:test1/foo:pull,push",
		keppel.CanPullFromAccount,
		keppel.CanPushToAccount)

	blobContents := []byte("just some random data")
	blobDigest := "sha256:" + sha256Of(blobContents)

	//test failure cases: no such upload
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/blobs/uploads/b9ef33aa-7e2a-4fc8-8083-6b00601dab98", //bogus session ID
		Header:       map[string]string{"Authorization": "Bearer " + readOnlyToken},
		ExpectStatus: http.StatusNotFound,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrBlobUploadUnknown),
	}.Check(t, h)

	//test success case: upload without contents in it
	uploadURL, uploadUUID := getBlobUpload(t, h, token)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/blobs/uploads/" + uploadUUID,
		Header:       map[string]string{"Authorization": "Bearer " + readOnlyToken},
		ExpectStatus: http.StatusNoContent,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey:    test.VersionHeaderValue,
			"Blob-Upload-Session-Id": uploadUUID,
			"Content-Length":         "0",
			"Range":                  "0-0",
		},
		ExpectBody: assert.StringData(""),
	}.Check(t, h)

	//test success case: upload with contents in it
	resp, _ := assert.HTTPRequest{
		Method: "PATCH",
		Path:   uploadURL,
		Header: map[string]string{
			"Authorization": "Bearer " + token,
			"Content-Type":  "application/octet-stream",
		},
		Body:         assert.ByteData(blobContents),
		ExpectStatus: http.StatusAccepted,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Content-Length":      "0",
			"Range":               fmt.Sprintf("0-%d", len(blobContents)),
		},
	}.Check(t, h)
	uploadURL = resp.Header.Get("Location")

	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/blobs/uploads/" + uploadUUID,
		Header:       map[string]string{"Authorization": "Bearer " + readOnlyToken},
		ExpectStatus: http.StatusNoContent,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey:    test.VersionHeaderValue,
			"Blob-Upload-Session-Id": uploadUUID,
			"Content-Length":         "0",
			"Range":                  fmt.Sprintf("0-%d", len(blobContents)),
		},
		ExpectBody: assert.StringData(""),
	}.Check(t, h)

	//test failure case: finished upload should be cleaned up and not show up in GET anymore
	assert.HTTPRequest{
		Method:       "PUT",
		Path:         keppel.AppendQuery(uploadURL, url.Values{"digest": {blobDigest}}),
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusCreated,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/blobs/uploads/" + uploadUUID,
		Header:       map[string]string{"Authorization": "Bearer " + readOnlyToken},
		ExpectStatus: http.StatusNotFound,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrBlobUploadUnknown),
	}.Check(t, h)
}

func TestDeleteBlobUpload(t *testing.T) {
	h, _, db, ad, sd, _ := setup(t)
	token := getToken(t, h, ad, "repository:test1/foo:pull,push",
		keppel.CanPullFromAccount,
		keppel.CanPushToAccount)
	deleteToken := getToken(t, h, ad, "repository:test1/foo:delete",
		keppel.CanDeleteFromAccount)

	blobContents := []byte("just some random data")

	//test failure cases: no such upload
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/v2/test1/foo/blobs/uploads/b9ef33aa-7e2a-4fc8-8083-6b00601dab98", //bogus session ID
		Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
		ExpectStatus: http.StatusNotFound,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrBlobUploadUnknown),
	}.Check(t, h)

	//test deletion of upload with no contents in it
	_, uploadUUID := getBlobUpload(t, h, token)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/v2/test1/foo/blobs/uploads/" + uploadUUID,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   test.ErrorCode(keppel.ErrDenied),
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/v2/test1/foo/blobs/uploads/" + uploadUUID,
		Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
		ExpectStatus: http.StatusNoContent,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Content-Length":      "0",
		},
		ExpectBody: assert.StringData(""),
	}.Check(t, h)

	//test deletion of upload with contents in it
	uploadURL, uploadUUID := getBlobUpload(t, h, token)
	assert.HTTPRequest{
		Method: "PATCH",
		Path:   uploadURL,
		Header: map[string]string{
			"Authorization": "Bearer " + token,
			"Content-Type":  "application/octet-stream",
		},
		Body:         assert.ByteData(blobContents),
		ExpectStatus: http.StatusAccepted,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Content-Length":      "0",
			"Range":               fmt.Sprintf("0-%d", len(blobContents)),
		},
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/v2/test1/foo/blobs/uploads/" + uploadUUID,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   test.ErrorCode(keppel.ErrDenied),
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/v2/test1/foo/blobs/uploads/" + uploadUUID,
		Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
		ExpectStatus: http.StatusNoContent,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Content-Length":      "0",
		},
		ExpectBody: assert.StringData(""),
	}.Check(t, h)

	//since all uploads were eventually deleted, there should be nothing in the storage
	expectStorageEmpty(t, sd, db)
}

func TestDeleteBlob(t *testing.T) {
	h, _, _, ad, _, _ := setup(t)
	token := getToken(t, h, ad, "repository:test1/foo:pull,push",
		keppel.CanPullFromAccount,
		keppel.CanPushToAccount)
	deleteToken := getToken(t, h, ad, "repository:test1/foo:delete",
		keppel.CanDeleteFromAccount)
	otherRepoToken := getToken(t, h, ad, "repository:test1/bar:pull,push",
		keppel.CanPullFromAccount,
		keppel.CanPushToAccount)

	blobContents := []byte("just some random data")
	blobDigest := "sha256:" + sha256Of(blobContents)

	//test failure case: delete blob from non-existent repo
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/v2/test1/foo/blobs/" + blobDigest,
		Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
		ExpectStatus: http.StatusNotFound,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrNameUnknown),
	}.Check(t, h)

	//push a blob so that we can test its deletion
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

	//cross-mount the same blob in a different repo (the blob should not be
	//deleted from test1/bar when we delete it from test1/foo)
	assert.HTTPRequest{
		Method: "POST",
		Path:   "/v2/test1/bar/blobs/uploads/?from=test1%2Ffoo&mount=" + blobDigest,
		Header: map[string]string{
			"Authorization":  "Bearer " + otherRepoToken,
			"Content-Length": "0",
		},
		ExpectStatus: http.StatusCreated,
	}.Check(t, h)

	//the blob should now be visible in both repos
	expectBlobContents(t, h, token, "test1/foo", blobDigest, blobContents)
	expectBlobContents(t, h, otherRepoToken, "test1/bar", blobDigest, blobContents)

	//test failure case: no delete permission
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/v2/test1/foo/blobs/" + blobDigest,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusForbidden,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrDenied),
	}.Check(t, h)

	//test failure case: no such blob
	bogusDigest := "sha256:" + sha256Of([]byte("something else"))
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/v2/test1/foo/blobs/" + bogusDigest,
		Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
		ExpectStatus: http.StatusNotFound,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrBlobUnknown),
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/v2/test1/foo/blobs/thisisnotadigest",
		Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
		ExpectStatus: http.StatusBadRequest,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrDigestInvalid),
	}.Check(t, h)

	//we only had failed DELETEs until now, so the blob should still be there
	expectBlobContents(t, h, token, "test1/foo", blobDigest, blobContents)
	expectBlobContents(t, h, otherRepoToken, "test1/bar", blobDigest, blobContents)

	//test success case: delete the blob from the first repo
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/v2/test1/foo/blobs/" + blobDigest,
		Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
		ExpectStatus: http.StatusAccepted,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Content-Length":      "0",
		},
	}.Check(t, h)

	//after successful DELETE, the blob should be gone from test1/foo...
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/blobs/" + blobDigest,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusNotFound,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrBlobUnknown),
	}.Check(t, h)
	//...but still be visible in test1/bar
	expectBlobContents(t, h, otherRepoToken, "test1/bar", blobDigest, blobContents)
}

func TestCrossRepositoryBlobMount(t *testing.T) {
	h, _, _, ad, _, _ := setup(t)
	readOnlyToken := getToken(t, h, ad, "repository:test1/foo:pull,push",
		keppel.CanPullFromAccount)
	token := getToken(t, h, ad, "repository:test1/foo:pull,push",
		keppel.CanPullFromAccount,
		keppel.CanPushToAccount)
	otherRepoToken := getToken(t, h, ad, "repository:test1/bar:pull,push",
		keppel.CanPullFromAccount,
		keppel.CanPushToAccount)

	blobContents := []byte("just some random data")
	blobDigest := "sha256:" + sha256Of(blobContents)

	//upload a blob to test1/bar so that we can test mounting it to test1/foo
	assert.HTTPRequest{
		Method: "POST",
		Path:   "/v2/test1/bar/blobs/uploads/?digest=" + blobDigest,
		Header: map[string]string{
			"Authorization":  "Bearer " + otherRepoToken,
			"Content-Length": strconv.Itoa(len(blobContents)),
			"Content-Type":   "application/octet-stream",
		},
		Body:         assert.ByteData(blobContents),
		ExpectStatus: http.StatusCreated,
	}.Check(t, h)

	//test failure cases: token does not have push access
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/v2/test1/foo/blobs/uploads/?from=test1/bar&mount=" + blobDigest,
		Header:       map[string]string{"Authorization": "Bearer " + readOnlyToken},
		ExpectStatus: http.StatusForbidden,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrDenied),
	}.Check(t, h)

	//test failure cases: source repo does not exist
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/v2/test1/foo/blobs/uploads/?from=test1/qux&mount=" + blobDigest,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusNotFound,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrNameUnknown),
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/v2/test1/foo/blobs/uploads/?from=test1/:qux&mount=" + blobDigest,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusBadRequest,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrNameInvalid),
	}.Check(t, h)

	//test failure cases: cannot mount across accounts
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/v2/test1/foo/blobs/uploads/?from=test2/foo&mount=" + blobDigest,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusMethodNotAllowed,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrUnsupported),
	}.Check(t, h)

	//test failure cases: digest is malformed or wrong
	bogusDigest := "sha256:" + sha256Of([]byte("something else"))
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/v2/test1/foo/blobs/uploads/?from=test1/bar&mount=wrong",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusBadRequest,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrDigestInvalid),
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/v2/test1/foo/blobs/uploads/?from=test1/bar&mount=" + bogusDigest,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusNotFound,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrBlobUnknown),
	}.Check(t, h)

	//since these all failed, the blob should not be available in test1/foo yet
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/blobs/" + blobDigest,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusNotFound,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrBlobUnknown),
	}.Check(t, h)

	//test success case
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/v2/test1/foo/blobs/uploads/?from=test1/bar&mount=" + blobDigest,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusCreated,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Content-Length":      "0",
			"Location":            "/v2/test1/foo/blobs/" + blobDigest,
		},
	}.Check(t, h)

	//now the blob should be available in both the original and the new repo
	expectBlobContents(t, h, token, "test1/foo", blobDigest, blobContents)
	expectBlobContents(t, h, otherRepoToken, "test1/bar", blobDigest, blobContents)
}
