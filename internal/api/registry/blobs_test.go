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

package registryv2_test

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
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler
		readOnlyToken := s.GetToken(t, "repository:test1/foo:pull")
		token := s.GetToken(t, "repository:test1/foo:pull,push")

		blob := test.NewBytes([]byte("just some random data"))

		//test failure cases: token does not have push access
		assert.HTTPRequest{
			Method: "POST",
			Path:   "/v2/test1/foo/blobs/uploads/?digest=" + blob.Digest.String(),
			Header: map[string]string{
				"Authorization":  "Bearer " + readOnlyToken,
				"Content-Length": strconv.Itoa(len(blob.Contents)),
				"Content-Type":   "application/octet-stream",
			},
			Body:         assert.ByteData(blob.Contents),
			ExpectStatus: http.StatusUnauthorized,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   test.ErrorCode(keppel.ErrDenied),
		}.Check(t, h)

		//test failure cases: account is in maintenance
		testWithAccountInMaintenance(t, s.DB, "test1", func() {
			assert.HTTPRequest{
				Method: "POST",
				Path:   "/v2/test1/foo/blobs/uploads/?digest=" + blob.Digest.String(),
				Header: map[string]string{
					"Authorization":  "Bearer " + token,
					"Content-Length": strconv.Itoa(len(blob.Contents)),
					"Content-Type":   "application/octet-stream",
				},
				Body:         assert.ByteData(blob.Contents),
				ExpectStatus: http.StatusMethodNotAllowed,
				ExpectHeader: test.VersionHeader,
				ExpectBody: test.ErrorCodeWithMessage{
					Code:    keppel.ErrUnsupported,
					Message: "account is in maintenance",
				},
			}.Check(t, h)
		})

		//test failure cases: digest is wrong
		for _, wrongDigest := range []string{"wrong", "sha256:" + sha256Of([]byte("something else"))} {
			assert.HTTPRequest{
				Method: "POST",
				Path:   "/v2/test1/foo/blobs/uploads/?digest=" + wrongDigest,
				Header: map[string]string{
					"Authorization":  "Bearer " + token,
					"Content-Length": strconv.Itoa(len(blob.Contents)),
					"Content-Type":   "application/octet-stream",
				},
				Body:         assert.ByteData(blob.Contents),
				ExpectStatus: http.StatusBadRequest,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrDigestInvalid),
			}.Check(t, h)
		}

		//test failure cases: Content-Length is wrong
		for _, wrongLength := range []string{"", "wrong", "1337"} {
			assert.HTTPRequest{
				Method: "POST",
				Path:   "/v2/test1/foo/blobs/uploads/?digest=" + blob.Digest.String(),
				Header: map[string]string{
					"Authorization":  "Bearer " + token,
					"Content-Length": wrongLength,
					"Content-Type":   "application/octet-stream",
				},
				Body:         assert.ByteData(blob.Contents),
				ExpectStatus: http.StatusBadRequest,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrSizeInvalid),
			}.Check(t, h)
		}

		//test failure cases: cannot upload manifest via the anycast API
		if currentlyWithAnycast {
			assert.HTTPRequest{
				Method: "POST",
				Path:   "/v2/test1/foo/blobs/uploads/?digest=" + blob.Digest.String(),
				Header: map[string]string{
					"Authorization":     "Bearer " + token,
					"Content-Length":    strconv.Itoa(len(blob.Contents)),
					"Content-Type":      "application/octet-stream",
					"X-Forwarded-Host":  s.Config.AnycastAPIPublicHostname,
					"X-Forwarded-Proto": "https",
				},
				Body:         assert.ByteData(blob.Contents),
				ExpectStatus: http.StatusMethodNotAllowed,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrUnsupported),
			}.Check(t, h)
		}

		//failed requests should not retain anything in the storage
		expectStorageEmpty(t, s.SD, s.DB)

		//test success case twice: should look the same also in the second pass
		for range []int{1, 2} {
			//test success case
			assert.HTTPRequest{
				Method: "POST",
				Path:   "/v2/test1/foo/blobs/uploads/?digest=" + blob.Digest.String(),
				Header: map[string]string{
					"Authorization":  "Bearer " + token,
					"Content-Length": strconv.Itoa(len(blob.Contents)),
					"Content-Type":   "application/octet-stream",
				},
				Body:         assert.ByteData(blob.Contents),
				ExpectStatus: http.StatusCreated,
				ExpectHeader: map[string]string{
					test.VersionHeaderKey: test.VersionHeaderValue,
					"Content-Length":      "0",
					"Location":            "/v2/test1/foo/blobs/" + blob.Digest.String(),
				},
			}.Check(t, h)

			//validate that the blob was stored at the specified location
			expectBlobExists(t, h, token, "test1/foo", blob, nil)
		}

		//test GET via anycast
		if currentlyWithAnycast {
			testWithReplica(t, s, "on_first_use", func(firstPass bool, s2 test.Setup) {
				testAnycast(t, firstPass, s2.DB, func() {
					h2 := s2.Handler
					anycastToken := s.GetAnycastToken(t, "repository:test1/foo:pull")
					anycastHeaders := map[string]string{
						"X-Forwarded-Host":  s.Config.AnycastAPIPublicHostname,
						"X-Forwarded-Proto": "https",
					}
					expectBlobExists(t, h, anycastToken, "test1/foo", blob, anycastHeaders)
					expectBlobExists(t, h2, anycastToken, "test1/foo", blob, anycastHeaders)
				})
			})
		}

		//test GET with anonymous user (fails unless a pull_anonymous RBAC policy is set up)
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/v2/test1/foo/blobs/" + blob.Digest.String(),
			Header:       test.AddHeadersForCorrectAuthChallenge(nil),
			ExpectStatus: http.StatusUnauthorized,
			ExpectHeader: map[string]string{
				test.VersionHeaderKey: test.VersionHeaderValue,
				"Www-Authenticate":    `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="repository:test1/foo:pull"`,
			},
		}.Check(t, h)
		_, err := s.DB.Exec(
			`INSERT INTO rbac_policies (account_name, match_repository, match_username, can_anon_pull) VALUES ('test1', 'foo', '', TRUE)`,
		)
		if err != nil {
			t.Fatal(err.Error())
		}
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/v2/test1/foo/blobs/" + blob.Digest.String(),
			ExpectStatus: http.StatusOK,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   assert.ByteData(blob.Contents),
		}.Check(t, h)
		_, err = s.DB.Exec(`DELETE FROM rbac_policies`)
		if err != nil {
			t.Fatal(err.Error())
		}
	})
}

func TestBlobStreamedAndChunkedUpload(t *testing.T) {
	//run everything in this testcase once for streamed upload and once for chunked upload
	for _, isChunked := range []bool{false, true} {
		testWithPrimary(t, nil, func(s test.Setup) {
			h := s.Handler
			readOnlyToken := s.GetToken(t, "repository:test1/foo:pull")
			token := s.GetToken(t, "repository:test1/foo:pull,push")

			blob := test.NewBytes([]byte("just some random data"))

			//shorthand for a common header structure that appears in many requests below
			getHeadersForPATCH := func(offset, length int) map[string]string {
				hdr := map[string]string{
					"Authorization": "Bearer " + token,
					"Content-Type":  "application/octet-stream",
				}
				//for streamed upload, Content-Range and Content-Length are omitted
				if isChunked {
					hdr["Content-Range"] = fmt.Sprintf("%d-%d", offset, offset+length-1)
					hdr["Content-Length"] = strconv.Itoa(length)
				}
				return hdr
			}

			//create the "test1/foo" repository to ensure that we don't just always hit
			//NAME_UNKNOWN errors
			_, err := keppel.FindOrCreateRepository(s.DB, "foo", keppel.Account{Name: "test1"})
			if err != nil {
				t.Fatal(err.Error())
			}

			//test failure cases during POST: anonymous is not allowed, should yield an auth challenge
			assert.HTTPRequest{
				Method: "POST",
				Path:   "/v2/test1/foo/blobs/uploads/",
				Header: test.AddHeadersForCorrectAuthChallenge(map[string]string{
					"Content-Length": strconv.Itoa(len(blob.Contents)),
					"Content-Type":   "application/octet-stream",
				}),
				Body:         assert.ByteData(blob.Contents),
				ExpectStatus: http.StatusUnauthorized,
				ExpectHeader: map[string]string{
					test.VersionHeaderKey: test.VersionHeaderValue,
					"Www-Authenticate":    `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="repository:test1/foo:pull,push"`,
				},
			}.Check(t, h)

			//test failure cases during POST: token does not have push access
			assert.HTTPRequest{
				Method: "POST",
				Path:   "/v2/test1/foo/blobs/uploads/",
				Header: map[string]string{
					"Authorization":  "Bearer " + readOnlyToken,
					"Content-Length": strconv.Itoa(len(blob.Contents)),
					"Content-Type":   "application/octet-stream",
				},
				Body:         assert.ByteData(blob.Contents),
				ExpectStatus: http.StatusUnauthorized,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrDenied),
			}.Check(t, h)

			//test failure cases during POST: account is in maintenance
			testWithAccountInMaintenance(t, s.DB, "test1", func() {
				assert.HTTPRequest{
					Method: "POST",
					Path:   "/v2/test1/foo/blobs/uploads/",
					Header: map[string]string{
						"Authorization":  "Bearer " + token,
						"Content-Length": strconv.Itoa(len(blob.Contents)),
						"Content-Type":   "application/octet-stream",
					},
					Body:         assert.ByteData(blob.Contents),
					ExpectStatus: http.StatusMethodNotAllowed,
					ExpectHeader: test.VersionHeader,
					ExpectBody: test.ErrorCodeWithMessage{
						Code:    keppel.ErrUnsupported,
						Message: "account is in maintenance",
					},
				}.Check(t, h)
			})

			//test failure cases during PATCH: bogus session ID
			assert.HTTPRequest{
				Method:       "PATCH",
				Path:         "/v2/test1/foo/blobs/uploads/b9ef33aa-7e2a-4fc8-8083-6b00601dab98", //bogus session ID
				Header:       getHeadersForPATCH(0, len(blob.Contents)),
				Body:         assert.ByteData(blob.Contents),
				ExpectStatus: http.StatusNotFound,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrBlobUploadUnknown),
			}.Check(t, h)

			//test failure cases during PATCH: unexpected session state (the first PATCH
			//request should not contain session state)
			assert.HTTPRequest{
				Method:       "PATCH",
				Path:         keppel.AppendQuery(getBlobUploadURL(t, h, token, "test1/foo"), url.Values{"state": {"unexpected"}}),
				Header:       getHeadersForPATCH(0, len(blob.Contents)),
				Body:         assert.ByteData(blob.Contents),
				ExpectStatus: http.StatusBadRequest,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrBlobUploadInvalid),
			}.Check(t, h)

			//test failure cases during PATCH: malformed session state (this requires a
			//successful PATCH first, otherwise the API would not expect to find session
			//state in our request)
			uploadURL := getBlobUploadURL(t, h, token, "test1/foo")
			assert.HTTPRequest{
				Method:       "PATCH",
				Path:         uploadURL,
				Header:       getHeadersForPATCH(0, len(blob.Contents)),
				Body:         assert.ByteData(blob.Contents),
				ExpectStatus: http.StatusAccepted,
			}.Check(t, h)
			assert.HTTPRequest{
				Method:       "PATCH",
				Path:         keppel.AppendQuery(uploadURL, url.Values{"state": {"unexpected"}}),
				Header:       getHeadersForPATCH(len(blob.Contents), len(blob.Contents)),
				Body:         assert.ByteData(blob.Contents),
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
					chunk1, chunk2 := blob.Contents[0:10], blob.Contents[10:15]
					resp, _ := assert.HTTPRequest{
						Method:       "PATCH",
						Path:         getBlobUploadURL(t, h, token, "test1/foo"),
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
						ExpectStatus: http.StatusRequestedRangeNotSatisfiable,
						ExpectHeader: test.VersionHeader,
						ExpectBody:   test.ErrorCode(keppel.ErrSizeInvalid),
					}.Check(t, h)
				}
				//NOTE: The correct headers would be Content-Range: 10-14 and Content-Length: 5.
				testWrongContentRangeAndOrLength("10-13", "4")                         //both consistently wrong
				testWrongContentRangeAndOrLength("10-14", "6")                         //only Content-Length wrong
				testWrongContentRangeAndOrLength("10-15", "5")                         //only Content-Range wrong
				testWrongContentRangeAndOrLength("8-12", "5")                          //consistent, but wrong offset
				testWrongContentRangeAndOrLength("10-14", "")                          //Content-Length missing
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
					Path:         getBlobUploadURL(t, h, token, "test1/foo"),
					Header:       getHeadersForPATCH(0, len(blob.Contents)),
					Body:         assert.ByteData(blob.Contents),
					ExpectStatus: http.StatusAccepted,
				}.Check(t, h)
				uploadURL := resp.Header.Get("Location") //nolint:govet
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
				chunk1, chunk2 := blob.Contents[0:10], blob.Contents[10:]
				resp, _ := assert.HTTPRequest{
					Method:       "PATCH",
					Path:         getBlobUploadURL(t, h, token, "test1/foo"),
					Header:       getHeadersForPATCH(0, len(chunk1)),
					Body:         assert.ByteData(chunk1),
					ExpectStatus: http.StatusAccepted,
				}.Check(t, h)
				uploadURL := resp.Header.Get("Location") //nolint:govet

				//when Content-Length is missing or 0, the request body will just be
				//ignored and the validation will fail later when the digest does not match
				//because of the missing chunk
				expectedError := test.ErrorCode(keppel.ErrSizeInvalid)
				expectedStatus := http.StatusRequestedRangeNotSatisfiable
				if wrongContentLength == "" || wrongContentLength == "0" {
					expectedError = test.ErrorCode(keppel.ErrDigestInvalid)
					expectedStatus = http.StatusBadRequest
				}

				assert.HTTPRequest{
					Method: "PUT",
					Path:   keppel.AppendQuery(uploadURL, url.Values{"digest": {blob.Digest.String()}}),
					Header: map[string]string{
						"Authorization":  "Bearer " + token,
						"Content-Length": wrongContentLength,
						"Content-Type":   "application/octet-stream",
					},
					Body:         assert.ByteData(chunk2),
					ExpectStatus: expectedStatus,
					ExpectHeader: test.VersionHeader,
					ExpectBody:   expectedError,
				}.Check(t, h)

				if t.Failed() {
					t.Fatalf("fails on CL %q", wrongContentLength)
				}
			}

			//failed requests should not retain anything in the storage
			expectStorageEmpty(t, s.SD, s.DB)

			//test success case twice: should look the same also in the second pass
			for range []int{1, 2} {
				//test success case (with multiple chunks!)
				uploadURL = getBlobUploadURL(t, h, token, "test1/foo")
				progress := 0
				for _, chunk := range bytes.SplitAfter(blob.Contents, []byte(" ")) {
					progress += len(chunk)

					if progress == len(blob.Contents) {
						//send the last chunk with the final PUT request
						assert.HTTPRequest{
							Method: "PUT",
							Path:   keppel.AppendQuery(uploadURL, url.Values{"digest": {blob.Digest.String()}}),
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
								"Location":            "/v2/test1/foo/blobs/" + blob.Digest.String(),
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
								"Range":               fmt.Sprintf("0-%d", progress-1),
							},
						}.Check(t, h)
						uploadURL = resp.Header.Get("Location")
					}
				}

				//validate that the blob was stored at the specified location
				expectBlobExists(t, h, token, "test1/foo", blob, nil)
			}

			if t.Failed() {
				t.FailNow()
			}
		})
	}
}

func TestGetBlobUpload(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler
		//NOTE: We only use the read-write token for driving the blob upload through
		//its various stages. All the GET requests use the read-only token to verify
		//that read-only tokens work here.
		readOnlyToken := s.GetToken(t, "repository:test1/foo:pull")
		token := s.GetToken(t, "repository:test1/foo:pull,push")

		blob := test.NewBytes([]byte("just some random data"))

		//create the "test1/foo" repository to ensure that we don't just always hit
		//NAME_UNKNOWN errors
		_, err := keppel.FindOrCreateRepository(s.DB, "foo", keppel.Account{Name: "test1"})
		if err != nil {
			t.Fatal(err.Error())
		}

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
		uploadURL, uploadUUID := getBlobUpload(t, h, token, "test1/foo")
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/v2/test1/foo/blobs/uploads/" + uploadUUID,
			Header:       map[string]string{"Authorization": "Bearer " + readOnlyToken},
			ExpectStatus: http.StatusNoContent,
			ExpectHeader: map[string]string{
				test.VersionHeaderKey:    test.VersionHeaderValue,
				"Blob-Upload-Session-Id": uploadUUID,
				"Content-Length":         "0",
				"Location":               uploadURL,
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
			Body:         assert.ByteData(blob.Contents),
			ExpectStatus: http.StatusAccepted,
			ExpectHeader: map[string]string{
				test.VersionHeaderKey: test.VersionHeaderValue,
				"Content-Length":      "0",
				"Range":               fmt.Sprintf("0-%d", len(blob.Contents)-1),
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
				"Range":                  fmt.Sprintf("0-%d", len(blob.Contents)-1),
				//This does not show "Location" because we don't have a way to recover
				//the digest state that's included in the query part of `uploadURL`.
			},
			ExpectBody: assert.StringData(""),
		}.Check(t, h)

		assert.HTTPRequest{
			Method:       "GET",
			Path:         uploadURL,
			Header:       map[string]string{"Authorization": "Bearer " + readOnlyToken},
			ExpectStatus: http.StatusNoContent,
			ExpectHeader: map[string]string{
				test.VersionHeaderKey:    test.VersionHeaderValue,
				"Blob-Upload-Session-Id": uploadUUID,
				"Content-Length":         "0",
				"Range":                  fmt.Sprintf("0-%d", len(blob.Contents)-1),
				//This DOES show "Location" (as the OCI Distribution Spec demands)
				//since we have the digest state available from the request URL.
				"Location": uploadURL,
			},
			ExpectBody: assert.StringData(""),
		}.Check(t, h)

		//test failure case: cannot inspect upload via the anycast API
		if currentlyWithAnycast {
			assert.HTTPRequest{
				Method: "GET",
				Path:   "/v2/test1/foo/blobs/uploads/" + uploadUUID,
				Header: map[string]string{
					"Authorization":     "Bearer " + readOnlyToken,
					"X-Forwarded-Host":  s.Config.AnycastAPIPublicHostname,
					"X-Forwarded-Proto": "https",
				},
				ExpectStatus: http.StatusMethodNotAllowed,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrUnsupported),
			}.Check(t, h)
		}

		//test failure case: finished upload should be cleaned up and not show up in GET anymore
		assert.HTTPRequest{
			Method:       "PUT",
			Path:         keppel.AppendQuery(uploadURL, url.Values{"digest": {blob.Digest.String()}}),
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
	})
}

func TestDeleteBlobUpload(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler
		token := s.GetToken(t, "repository:test1/foo:pull,push")
		deleteToken := s.GetToken(t, "repository:test1/foo:delete")

		blobContents := []byte("just some random data")

		//create the "test1/foo" repository to ensure that we don't just always hit
		//NAME_UNKNOWN errors
		_, err := keppel.FindOrCreateRepository(s.DB, "foo", keppel.Account{Name: "test1"})
		if err != nil {
			t.Fatal(err.Error())
		}

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
		_, uploadUUID := getBlobUpload(t, h, token, "test1/foo")
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/v2/test1/foo/blobs/uploads/" + uploadUUID,
			Header:       map[string]string{"Authorization": "Bearer " + token},
			ExpectStatus: http.StatusUnauthorized,
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
		uploadURL, uploadUUID := getBlobUpload(t, h, token, "test1/foo")
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
				"Range":               fmt.Sprintf("0-%d", len(blobContents)-1),
			},
		}.Check(t, h)
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/v2/test1/foo/blobs/uploads/" + uploadUUID,
			Header:       map[string]string{"Authorization": "Bearer " + token},
			ExpectStatus: http.StatusUnauthorized,
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
		expectStorageEmpty(t, s.SD, s.DB)
	})
}

func TestDeleteBlob(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler
		token := s.GetToken(t, "repository:test1/foo:pull,push")
		deleteToken := s.GetToken(t, "repository:test1/foo:delete")
		otherRepoToken := s.GetToken(t, "repository:test1/bar:pull,push")

		blob := test.NewBytes([]byte("just some random data"))

		//test failure case: delete blob from non-existent repo
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/v2/test1/foo/blobs/" + blob.Digest.String(),
			Header:       map[string]string{"Authorization": "Bearer " + deleteToken},
			ExpectStatus: http.StatusNotFound,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   test.ErrorCode(keppel.ErrNameUnknown),
		}.Check(t, h)

		//push a blob so that we can test its deletion
		blob.MustUpload(t, s, fooRepoRef)

		//cross-mount the same blob in a different repo (the blob should not be
		//deleted from test1/bar when we delete it from test1/foo)
		assert.HTTPRequest{
			Method: "POST",
			Path:   "/v2/test1/bar/blobs/uploads/?from=test1%2Ffoo&mount=" + blob.Digest.String(),
			Header: map[string]string{
				"Authorization":  "Bearer " + otherRepoToken,
				"Content-Length": "0",
			},
			ExpectStatus: http.StatusCreated,
		}.Check(t, h)

		//the blob should now be visible in both repos
		expectBlobExists(t, h, token, "test1/foo", blob, nil)
		expectBlobExists(t, h, otherRepoToken, "test1/bar", blob, nil)

		//test failure case: no delete permission
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/v2/test1/foo/blobs/" + blob.Digest.String(),
			Header:       map[string]string{"Authorization": "Bearer " + token},
			ExpectStatus: http.StatusUnauthorized,
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
		expectBlobExists(t, h, token, "test1/foo", blob, nil)
		expectBlobExists(t, h, otherRepoToken, "test1/bar", blob, nil)

		//test success case: delete the blob from the first repo
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/v2/test1/foo/blobs/" + blob.Digest.String(),
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
			Path:         "/v2/test1/foo/blobs/" + blob.Digest.String(),
			Header:       map[string]string{"Authorization": "Bearer " + token},
			ExpectStatus: http.StatusNotFound,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   test.ErrorCode(keppel.ErrBlobUnknown),
		}.Check(t, h)
		//...but still be visible in test1/bar
		expectBlobExists(t, h, otherRepoToken, "test1/bar", blob, nil)
	})
}

func TestCrossRepositoryBlobMount(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		h := s.Handler
		readOnlyToken := s.GetToken(t, "repository:test1/foo:pull")
		token := s.GetToken(t, "repository:test1/foo:pull,push")
		otherRepoToken := s.GetToken(t, "repository:test1/bar:pull,push")

		blob := test.NewBytes([]byte("just some random data"))

		//upload a blob to test1/bar so that we can test mounting it to test1/foo
		blob.MustUpload(t, s, barRepoRef)

		//test failure cases: token does not have push access
		assert.HTTPRequest{
			Method:       "POST",
			Path:         "/v2/test1/foo/blobs/uploads/?from=test1/bar&mount=" + blob.Digest.String(),
			Header:       map[string]string{"Authorization": "Bearer " + readOnlyToken},
			ExpectStatus: http.StatusUnauthorized,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   test.ErrorCode(keppel.ErrDenied),
		}.Check(t, h)

		//test failure cases: source repo does not exist
		assert.HTTPRequest{
			Method:       "POST",
			Path:         "/v2/test1/foo/blobs/uploads/?from=test1/qux&mount=" + blob.Digest.String(),
			Header:       map[string]string{"Authorization": "Bearer " + token},
			ExpectStatus: http.StatusNotFound,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   test.ErrorCode(keppel.ErrNameUnknown),
		}.Check(t, h)
		assert.HTTPRequest{
			Method:       "POST",
			Path:         "/v2/test1/foo/blobs/uploads/?from=test1/:qux&mount=" + blob.Digest.String(),
			Header:       map[string]string{"Authorization": "Bearer " + token},
			ExpectStatus: http.StatusBadRequest,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   test.ErrorCode(keppel.ErrNameInvalid),
		}.Check(t, h)

		//test failure cases: cannot mount across accounts
		assert.HTTPRequest{
			Method:       "POST",
			Path:         "/v2/test1/foo/blobs/uploads/?from=test2/foo&mount=" + blob.Digest.String(),
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
			Path:         "/v2/test1/foo/blobs/" + blob.Digest.String(),
			Header:       map[string]string{"Authorization": "Bearer " + token},
			ExpectStatus: http.StatusNotFound,
			ExpectHeader: test.VersionHeader,
			ExpectBody:   test.ErrorCode(keppel.ErrBlobUnknown),
		}.Check(t, h)

		//test success case
		assert.HTTPRequest{
			Method:       "POST",
			Path:         "/v2/test1/foo/blobs/uploads/?from=test1/bar&mount=" + blob.Digest.String(),
			Header:       map[string]string{"Authorization": "Bearer " + token},
			ExpectStatus: http.StatusCreated,
			ExpectHeader: map[string]string{
				test.VersionHeaderKey: test.VersionHeaderValue,
				"Content-Length":      "0",
				"Location":            "/v2/test1/foo/blobs/" + blob.Digest.String(),
			},
		}.Check(t, h)

		//now the blob should be available in both the original and the new repo
		expectBlobExists(t, h, token, "test1/foo", blob, nil)
		expectBlobExists(t, h, otherRepoToken, "test1/bar", blob, nil)
	})
}
