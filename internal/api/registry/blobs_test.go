// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"bytes"
	"cmp"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/sapcc/go-bits/httptest"
	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

// uploadingBlobMonolithically is a custom RequestOption for the "POST blob upload" endpoint.
func uploadingBlobMonolithically(blob test.Bytes) httptest.RequestOption {
	return httptest.MergeRequestOptions(
		httptest.WithHeader("Content-Length", strconv.Itoa(len(blob.Contents))),
		httptest.WithHeader("Content-Type", blob.MediaType),
		httptest.WithBody(bytes.NewReader(blob.Contents)),
	)
}

func TestBlobMonolithicUpload(t *testing.T) {
	ctx := t.Context()

	testWithPrimary(t, nil, func(s test.Setup) {
		readOnlyTokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull")
		tokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull,push")

		blob := test.NewBytes([]byte("just some random data"))

		// test failure cases: token does not have push access
		s.RespondTo(ctx, "POST /v2/test1/foo/blobs/uploads/?digest="+blob.Digest.String(),
			httptest.WithHeaders(readOnlyTokenHeaders),
			uploadingBlobMonolithically(blob),
		).ExpectJSON(t, http.StatusUnauthorized, test.ErrorCode(keppel.ErrDenied))

		// test failure cases: account is in maintenance
		testWithAccountIsDeleting(t, s.DB, "test1", func() {
			s.RespondTo(ctx, "POST /v2/test1/foo/blobs/uploads/?digest="+blob.Digest.String(),
				httptest.WithHeaders(tokenHeaders),
				uploadingBlobMonolithically(blob),
			).ExpectJSON(t, http.StatusMethodNotAllowed, test.ErrorCodeWithMessage{
				Code:    keppel.ErrUnsupported,
				Message: "account is being deleted",
			})
		})

		// test failure cases: digest is wrong
		for _, wrongDigest := range []string{"wrong", test.DeterministicDummyDigest(1).String()} {
			s.RespondTo(ctx, "POST /v2/test1/foo/blobs/uploads/?digest="+wrongDigest,
				httptest.WithHeaders(tokenHeaders),
				uploadingBlobMonolithically(blob),
			).ExpectJSON(t, http.StatusBadRequest, test.ErrorCode(keppel.ErrDigestInvalid))
		}

		// test failure cases: Content-Length is wrong
		for _, wrongLength := range []string{"", "wrong", "1337"} {
			s.RespondTo(ctx, "POST /v2/test1/foo/blobs/uploads/?digest="+blob.Digest.String(),
				httptest.WithHeaders(tokenHeaders),
				uploadingBlobMonolithically(blob),
				httptest.WithHeader("Content-Length", wrongLength), // overrides Content-Length within uploadingBlobMonolithically()
			).ExpectJSON(t, http.StatusBadRequest, test.ErrorCode(keppel.ErrSizeInvalid))
		}

		// test failure cases: cannot upload manifest via the anycast API
		if currentlyWithAnycast {
			s.RespondTo(ctx, "POST /v2/test1/foo/blobs/uploads/?digest="+blob.Digest.String(),
				httptest.WithHeaders(tokenHeaders),
				uploadingBlobMonolithically(blob),
				httptest.WithHeaders(http.Header{
					"X-Forwarded-Host":  {s.Config.AnycastAPIPublicHostname},
					"X-Forwarded-Proto": {"https"},
				}),
			).ExpectJSON(t, http.StatusMethodNotAllowed, test.ErrorCode(keppel.ErrUnsupported))
		}

		// failed requests should not retain anything in the storage
		expectStorageEmpty(t, s.SD, s.DB)

		// test success case twice: should look the same also in the second pass
		for range []int{1, 2} {
			// test success case
			s.RespondTo(ctx, "POST /v2/test1/foo/blobs/uploads/?digest="+blob.Digest.String(),
				httptest.WithHeaders(tokenHeaders),
				uploadingBlobMonolithically(blob),
			).ExpectHeaders(t, http.Header{
				"Content-Length": {"0"},
				"Location":       {"/v2/test1/foo/blobs/" + blob.Digest.String()},
			}).ExpectStatus(t, http.StatusCreated)

			// validate that the blob was stored at the specified location
			expectBlobExists(t, s, tokenHeaders, "test1/foo", blob)
		}

		// test GET via anycast
		if currentlyWithAnycast {
			testWithReplica(t, s, "on_first_use", func(firstPass bool, s2 test.Setup) {
				testAnycast(t, firstPass, s2.DB, func() {
					anycastTokenHeaders := s.GetAnycastTokenHeaders(t, "repository:test1/foo:pull")
					expectBlobExists(t, s, anycastTokenHeaders, "test1/foo", blob)
					expectBlobExists(t, s2, anycastTokenHeaders, "test1/foo", blob)
				})
			})
		}

		// test GET with anonymous user (fails unless a pull_anonymous RBAC policy is set up)
		s.RespondTo(ctx, "GET /v2/test1/foo/blobs/"+blob.Digest.String()).
			ExpectHeader(t, "Www-Authenticate", `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="repository:test1/foo:pull"`).
			ExpectStatus(t, http.StatusUnauthorized)

		test.MustExec(t, s.DB, `UPDATE accounts SET rbac_policies_json = $2 WHERE name = $1`, "test1",
			test.ToJSON([]keppel.RBACPolicy{{
				RepositoryPattern: "foo",
				Permissions:       []keppel.RBACPermission{keppel.RBACAnonymousPullPermission},
			}}),
		)

		s.RespondTo(ctx, "GET /v2/test1/foo/blobs/"+blob.Digest.String()).
			ExpectBody(t, http.StatusOK, blob.Contents)

		test.MustExec(t, s.DB, `UPDATE accounts SET rbac_policies_json = $2 WHERE name = $1`, "test1", "")
	})
}

func TestBlobStreamedAndChunkedUpload(t *testing.T) {
	ctx := t.Context()

	// run everything in this testcase once for streamed upload and once for chunked upload
	for _, isChunked := range []bool{false, true} {
		testWithPrimary(t, nil, func(s test.Setup) {
			readOnlyTokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull")
			tokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull,push")

			blob := test.NewBytes([]byte("just some random data"))

			// shorthand for a common header structure that appears in many requests below
			getHeadersForPATCH := func(offset, length int) http.Header {
				hdr := maps.Clone(tokenHeaders)
				hdr.Set("Content-Type", "application/octet-stream")
				// for streamed upload, Content-Range and Content-Length are omitted
				if isChunked {
					hdr.Set("Content-Range", fmt.Sprintf("%d-%d", offset, offset+length-1))
					hdr.Set("Content-Length", strconv.Itoa(length))
				}
				return hdr
			}

			// create the "test1/foo" repository to ensure that we don't just always hit
			// NAME_UNKNOWN errors
			_ = must.ReturnT(keppel.FindOrCreateRepository(s.DB, "foo", models.AccountName("test1")))(t)

			// test failure cases during POST: anonymous is not allowed, should yield an auth challenge
			s.RespondTo(ctx, "POST /v2/test1/foo/blobs/uploads/", uploadingBlobMonolithically(blob)).
				ExpectHeader(t, "Www-Authenticate", `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="repository:test1/foo:pull,push"`).
				ExpectStatus(t, http.StatusUnauthorized)

			// test failure cases during POST: token does not have push access
			s.RespondTo(ctx, "POST /v2/test1/foo/blobs/uploads/",
				httptest.WithHeaders(readOnlyTokenHeaders),
				uploadingBlobMonolithically(blob),
			).ExpectJSON(t, http.StatusUnauthorized, test.ErrorCode(keppel.ErrDenied))

			// test failure cases during POST: account is in maintenance
			testWithAccountIsDeleting(t, s.DB, "test1", func() {
				s.RespondTo(ctx, "POST /v2/test1/foo/blobs/uploads/",
					httptest.WithHeaders(tokenHeaders),
					uploadingBlobMonolithically(blob),
				).ExpectJSON(t, http.StatusMethodNotAllowed, test.ErrorCodeWithMessage{
					Code:    keppel.ErrUnsupported,
					Message: "account is being deleted",
				})
			})

			// test failure cases during PATCH: bogus session ID
			s.RespondTo(ctx, "PATCH /v2/test1/foo/blobs/uploads/b9ef33aa-7e2a-4fc8-8083-6b00601dab98", // bogus session ID
				httptest.WithHeaders(getHeadersForPATCH(0, len(blob.Contents))),
				httptest.WithBody(bytes.NewReader(blob.Contents)),
			).ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrBlobUploadUnknown))

			// test failure cases during PATCH: unexpected session state (the first PATCH
			// request should not contain session state)
			uploadURL := keppel.AppendQuery(getBlobUploadURL(t, s, tokenHeaders, "test1/foo"), url.Values{"state": {"unexpected"}})
			s.RespondTo(ctx, "PATCH "+uploadURL,
				httptest.WithHeaders(getHeadersForPATCH(0, len(blob.Contents))),
				httptest.WithBody(bytes.NewReader(blob.Contents)),
			).ExpectJSON(t, http.StatusBadRequest, test.ErrorCode(keppel.ErrBlobUploadInvalid))

			// upload with mismatched content length header to trigger a broken session state
			uploadURL = getBlobUploadURL(t, s, tokenHeaders, "test1/foo")
			s.RespondTo(ctx, "PATCH "+uploadURL,
				httptest.WithHeaders(getHeadersForPATCH(0, len(blob.Contents))),
				httptest.WithBody(bytes.NewReader(blob.Contents)),
			).ExpectStatus(t, http.StatusAccepted)

			s.RespondTo(ctx, "PATCH "+uploadURL,
				httptest.WithHeaders(getHeadersForPATCH(len(blob.Contents)-5, len(blob.Contents))),
				httptest.WithBody(bytes.NewReader(blob.Contents)),
			).ExpectStatus(t, http.StatusRequestedRangeNotSatisfiable)

			// test that Content-Length header matches which can only occur when doing chunked uploads
			if isChunked {
				uploadURL = getBlobUploadURL(t, s, tokenHeaders, "test1/foo")
				s.RespondTo(ctx, "PATCH "+uploadURL,
					httptest.WithHeaders(getHeadersForPATCH(0, len(blob.Contents)-10)),
					httptest.WithBody(bytes.NewReader(blob.Contents)),
				).ExpectJSON(t, http.StatusRequestedRangeNotSatisfiable, test.ErrorCodeWithMessage{
					Code:    keppel.ErrSizeInvalid,
					Message: "expected upload of 11 bytes, but request contained 21 bytes",
				})
			}

			// test failure cases during PATCH: malformed session state (this requires a
			// successful PATCH first, otherwise the API would not expect to find session
			// state in our request)
			uploadURL = getBlobUploadURL(t, s, tokenHeaders, "test1/foo")
			s.RespondTo(ctx, "PATCH "+uploadURL,
				httptest.WithHeaders(getHeadersForPATCH(0, len(blob.Contents))),
				httptest.WithBody(bytes.NewReader(blob.Contents)),
			).ExpectStatus(t, http.StatusAccepted)

			s.RespondTo(ctx, "PATCH "+keppel.AppendQuery(uploadURL, url.Values{"state": {"unexpected"}}),
				httptest.WithHeaders(getHeadersForPATCH(len(blob.Contents), len(blob.Contents))),
				httptest.WithBody(bytes.NewReader(blob.Contents)),
			).ExpectJSON(t, http.StatusBadRequest, test.ErrorCode(keppel.ErrBlobUploadInvalid))

			// test failure cases during PATCH: malformed Content-Range and/or
			// Content-Length (only for chunked upload; streamed upload does not have
			// these headers)
			if isChunked {
				testWrongContentRangeAndOrLength := func(contentRange, contentLength string) {
					t.Helper()
					// upload the blob contents in two chunks; we will trigger the error
					// condition in the second PATCH
					chunk1, chunk2 := blob.Contents[0:10], blob.Contents[10:15]
					var nextUploadURL string

					s.RespondTo(ctx, "PATCH "+getBlobUploadURL(t, s, tokenHeaders, "test1/foo"),
						httptest.WithHeaders(getHeadersForPATCH(0, len(chunk1))),
						httptest.WithBody(bytes.NewReader(chunk1)),
					).
						CaptureHeader("Location", &nextUploadURL).
						ExpectStatus(t, http.StatusAccepted)

					s.RespondTo(ctx, "PATCH "+nextUploadURL,
						httptest.WithHeaders(tokenHeaders),
						httptest.WithHeader("Content-Length", contentLength),
						httptest.WithHeader("Content-Range", contentRange),
						httptest.WithHeader("Content-Type", "application/octet-stream"),
						httptest.WithBody(bytes.NewReader(chunk2)),
					).ExpectJSON(t, http.StatusRequestedRangeNotSatisfiable, test.ErrorCode(keppel.ErrSizeInvalid))
				}

				//NOTE: The correct headers would be Content-Range: 10-14 and Content-Length: 5.
				testWrongContentRangeAndOrLength("10-13", "4")                         // both consistently wrong
				testWrongContentRangeAndOrLength("10-14", "6")                         // only Content-Length wrong
				testWrongContentRangeAndOrLength("10-15", "5")                         // only Content-Range wrong
				testWrongContentRangeAndOrLength("8-12", "5")                          // consistent, but wrong offset
				testWrongContentRangeAndOrLength("10-14", "")                          // Content-Length missing
				testWrongContentRangeAndOrLength("10", "5")                            // wrong format for Content-Range
				testWrongContentRangeAndOrLength("10-abc", "5")                        // even wronger format for Content-Range
				testWrongContentRangeAndOrLength("99999999999999999999999999-10", "5") // what are you doing?
				testWrongContentRangeAndOrLength("10-99999999999999999999999999", "5") // omg stop it!
			}

			// test failure cases during PUT: digest is missing or wrong
			for _, wrongDigest := range []string{"", "wrong", test.DeterministicDummyDigest(2).String()} {
				// upload all the blob contents at once (we're only interested in the final PUT)
				s.RespondTo(ctx, "PATCH "+getBlobUploadURL(t, s, tokenHeaders, "test1/foo"),
					httptest.WithHeaders(getHeadersForPATCH(0, len(blob.Contents))),
					httptest.WithBody(bytes.NewReader(blob.Contents)),
				).
					CaptureHeader("Location", &uploadURL).
					ExpectStatus(t, http.StatusAccepted)

				s.RespondTo(ctx, "PUT "+keppel.AppendQuery(uploadURL, url.Values{"digest": {wrongDigest}}),
					httptest.WithHeaders(tokenHeaders),
				).ExpectJSON(t, http.StatusBadRequest, test.ErrorCode(keppel.ErrDigestInvalid))
			}

			// test failure cases during PUT: broken Content-Length on PUT with content
			for _, wrongContentLength := range []string{"", "0", "1024"} {
				t.Run("Content-Length="+cmp.Or(wrongContentLength, "empty"), func(t *testing.T) {
					// upload the blob contents in two chunks, so that we have a chunk to send with PUT
					chunk1, chunk2 := blob.Contents[0:10], blob.Contents[10:]
					s.RespondTo(ctx, "PATCH "+getBlobUploadURL(t, s, tokenHeaders, "test1/foo"),
						httptest.WithHeaders(getHeadersForPATCH(0, len(chunk1))),
						httptest.WithBody(bytes.NewReader(chunk1)),
					).
						CaptureHeader("Location", &uploadURL).
						ExpectStatus(t, http.StatusAccepted)

					resp := s.RespondTo(ctx, "PUT "+keppel.AppendQuery(uploadURL, url.Values{"digest": {blob.Digest.String()}}),
						httptest.WithHeaders(tokenHeaders),
						httptest.WithHeader("Content-Length", wrongContentLength),
						httptest.WithHeader("Content-Type", "application/octet-stream"),
						httptest.WithBody(bytes.NewReader(chunk2)),
					)

					// when Content-Length is missing or 0, the request body will just be
					// ignored and the validation will fail later when the digest does not match
					// because of the missing chunk
					if wrongContentLength == "" || wrongContentLength == "0" {
						resp.ExpectJSON(t, http.StatusBadRequest, test.ErrorCode(keppel.ErrDigestInvalid))
					} else {
						resp.ExpectJSON(t, http.StatusRequestedRangeNotSatisfiable, test.ErrorCode(keppel.ErrSizeInvalid))
					}
				})
			}

			// failed requests should not retain anything in the storage
			expectStorageEmpty(t, s.SD, s.DB)

			// test success case twice: should look the same also in the second pass
			for range []int{1, 2} {
				// test success case (with multiple chunks!)
				uploadURL = getBlobUploadURL(t, s, tokenHeaders, "test1/foo")
				progress := 0
				for _, chunk := range bytes.SplitAfter(blob.Contents, []byte(" ")) {
					progress += len(chunk)

					if progress == len(blob.Contents) {
						// send the last chunk with the final PUT request
						s.RespondTo(ctx, "PUT "+keppel.AppendQuery(uploadURL, url.Values{"digest": {blob.Digest.String()}}),
							httptest.WithHeaders(tokenHeaders),
							httptest.WithHeader("Content-Length", strconv.Itoa(len(chunk))),
							httptest.WithHeader("Content-Type", "application/octet-stream"),
							httptest.WithBody(bytes.NewReader(chunk)),
						).ExpectHeaders(t, http.Header{
							"Content-Length": {"0"},
							"Location":       {"/v2/test1/foo/blobs/" + blob.Digest.String()},
						}).ExpectStatus(t, http.StatusCreated)
					} else {
						s.RespondTo(ctx, "PATCH "+uploadURL,
							httptest.WithHeaders(getHeadersForPATCH(progress-len(chunk), len(chunk))),
							httptest.WithBody(bytes.NewReader(chunk)),
						).ExpectHeaders(t, http.Header{
							"Content-Length": {"0"},
							"Range":          {fmt.Sprintf("0-%d", progress-1)},
						}).
							CaptureHeader("Location", &uploadURL).
							ExpectStatus(t, http.StatusAccepted)
					}
				}

				// validate that the blob was stored at the specified location
				expectBlobExists(t, s, tokenHeaders, "test1/foo", blob)
			}

			if t.Failed() {
				t.FailNow()
			}
		})
	}
}

func TestGetBlobUpload(t *testing.T) {
	ctx := t.Context()

	testWithPrimary(t, nil, func(s test.Setup) {
		//NOTE: We only use the read-write token for driving the blob upload through
		// its various stages. All the GET requests use the read-only token to verify
		// that read-only tokens work here.
		readOnlyTokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull")
		tokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull,push")

		blob := test.NewBytes([]byte("just some random data"))

		// create the "test1/foo" repository to ensure that we don't just always hit
		// NAME_UNKNOWN errors
		_ = must.ReturnT(keppel.FindOrCreateRepository(s.DB, "foo", models.AccountName("test1")))(t)

		// test failure cases: no such upload
		s.RespondTo(ctx, "GET /v2/test1/foo/blobs/uploads/b9ef33aa-7e2a-4fc8-8083-6b00601dab98", // bogus session ID
			httptest.WithHeaders(readOnlyTokenHeaders),
		).ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrBlobUploadUnknown))

		// test success case: upload without contents in it
		uploadURL, uploadUUID := getBlobUpload(t, s, tokenHeaders, "test1/foo")
		s.RespondTo(ctx, "GET /v2/test1/foo/blobs/uploads/"+uploadUUID,
			httptest.WithHeaders(readOnlyTokenHeaders),
		).ExpectHeaders(t, http.Header{
			"Blob-Upload-Session-Id": {uploadUUID},
			"Content-Length":         {"0"},
			"Location":               {uploadURL},
			"Range":                  {"0-0"},
		}).ExpectText(t, http.StatusNoContent, "")

		// test success case: upload with contents in it
		s.RespondTo(ctx, "PATCH "+uploadURL,
			httptest.WithHeaders(tokenHeaders),
			httptest.WithHeader("Content-Type", "application/octet-stream"),
			httptest.WithBody(bytes.NewReader(blob.Contents)),
		).ExpectHeaders(t, http.Header{
			"Content-Length": {"0"},
			"Range":          {fmt.Sprintf("0-%d", len(blob.Contents)-1)},
		}).
			CaptureHeader("Location", &uploadURL).
			ExpectStatus(t, http.StatusAccepted)

		s.RespondTo(ctx, "GET /v2/test1/foo/blobs/uploads/"+uploadUUID,
			httptest.WithHeaders(readOnlyTokenHeaders),
		).ExpectHeaders(t, http.Header{
			"Blob-Upload-Session-Id": {uploadUUID},
			"Content-Length":         {"0"},
			"Range":                  {fmt.Sprintf("0-%d", len(blob.Contents)-1)},
			// This does not show "Location" because we don't have a way to recover
			// the digest state that's included in the query part of `uploadURL`.
		}).ExpectText(t, http.StatusNoContent, "")

		s.RespondTo(ctx, "GET "+uploadURL,
			httptest.WithHeaders(readOnlyTokenHeaders),
		).ExpectHeaders(t, http.Header{
			"Blob-Upload-Session-Id": {uploadUUID},
			"Content-Length":         {"0"},
			"Range":                  {fmt.Sprintf("0-%d", len(blob.Contents)-1)},
			// This DOES show "Location" (as the OCI Distribution Spec demands)
			// since we have the digest state available from the request URL.
			"Location": {uploadURL},
		}).ExpectText(t, http.StatusNoContent, "")

		// test failure case: cannot inspect upload via the anycast API
		if currentlyWithAnycast {
			s.RespondTo(ctx, "GET /v2/test1/foo/blobs/uploads/"+uploadUUID,
				httptest.WithHeaders(readOnlyTokenHeaders),
				httptest.WithHeaders(http.Header{
					"X-Forwarded-Host":  {s.Config.AnycastAPIPublicHostname},
					"X-Forwarded-Proto": {"https"},
				}),
			).ExpectJSON(t, http.StatusMethodNotAllowed, test.ErrorCode(keppel.ErrUnsupported))
		}

		// test failure case: finished upload should be cleaned up and not show up in GET anymore
		s.RespondTo(ctx, "PUT "+keppel.AppendQuery(uploadURL, url.Values{"digest": {blob.Digest.String()}}),
			httptest.WithHeaders(tokenHeaders),
		).ExpectStatus(t, http.StatusCreated)

		s.RespondTo(ctx, "GET /v2/test1/foo/blobs/uploads/"+uploadUUID,
			httptest.WithHeaders(readOnlyTokenHeaders),
		).ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrBlobUploadUnknown))
	})
}

func TestDeleteBlobUpload(t *testing.T) {
	ctx := t.Context()

	testWithPrimary(t, nil, func(s test.Setup) {
		tokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull,push")
		deleteTokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:delete")

		blobContents := []byte("just some random data")

		// create the "test1/foo" repository to ensure that we don't just always hit
		// NAME_UNKNOWN errors
		_ = must.ReturnT(keppel.FindOrCreateRepository(s.DB, "foo", models.AccountName("test1")))(t)

		// test failure cases: no such upload
		s.RespondTo(ctx, "DELETE /v2/test1/foo/blobs/uploads/b9ef33aa-7e2a-4fc8-8083-6b00601dab98", // bogus session ID
			httptest.WithHeaders(deleteTokenHeaders),
		).ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrBlobUploadUnknown))

		// test deletion of upload with no contents in it
		_, uploadUUID := getBlobUpload(t, s, tokenHeaders, "test1/foo")
		s.RespondTo(ctx, "DELETE /v2/test1/foo/blobs/uploads/"+uploadUUID,
			httptest.WithHeaders(tokenHeaders),
		).ExpectJSON(t, http.StatusUnauthorized, test.ErrorCode(keppel.ErrDenied))

		s.RespondTo(ctx, "DELETE /v2/test1/foo/blobs/uploads/"+uploadUUID,
			httptest.WithHeaders(deleteTokenHeaders),
		).
			ExpectHeader(t, "Content-Length", "0").
			ExpectText(t, http.StatusNoContent, "")

		// test deletion of upload with contents in it
		uploadURL, uploadUUID := getBlobUpload(t, s, tokenHeaders, "test1/foo")
		s.RespondTo(ctx, "PATCH "+uploadURL,
			httptest.WithHeaders(tokenHeaders),
			httptest.WithHeader("Content-Type", "application/octet-stream"),
			httptest.WithBody(bytes.NewReader(blobContents)),
		).ExpectHeaders(t, http.Header{
			"Content-Length": {"0"},
			"Range":          {fmt.Sprintf("0-%d", len(blobContents)-1)},
		}).ExpectStatus(t, http.StatusAccepted)

		s.RespondTo(ctx, "DELETE /v2/test1/foo/blobs/uploads/"+uploadUUID,
			httptest.WithHeaders(tokenHeaders),
		).ExpectJSON(t, http.StatusUnauthorized, test.ErrorCode(keppel.ErrDenied))

		s.RespondTo(ctx, "DELETE /v2/test1/foo/blobs/uploads/"+uploadUUID,
			httptest.WithHeaders(deleteTokenHeaders),
		).
			ExpectHeader(t, "Content-Length", "0").
			ExpectText(t, http.StatusNoContent, "")

		// since all uploads were eventually deleted, there should be nothing in the storage
		expectStorageEmpty(t, s.SD, s.DB)
	})
}

func TestDeleteBlob(t *testing.T) {
	ctx := t.Context()

	testWithPrimary(t, nil, func(s test.Setup) {
		tokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull,push")
		deleteTokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:delete")
		otherRepoTokenHeaders := s.GetTokenHeaders(t, "repository:test1/bar:pull,push")

		blob := test.NewBytes([]byte("just some random data"))

		// test failure case: delete blob from non-existent repo
		s.RespondTo(ctx, "DELETE /v2/test1/foo/blobs/"+blob.Digest.String(),
			httptest.WithHeaders(deleteTokenHeaders),
		).ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrNameUnknown))

		// push a blob so that we can test its deletion
		blob.MustUpload(t, s, fooRepoRef)

		// cross-mount the same blob in a different repo (the blob should not be
		// deleted from test1/bar when we delete it from test1/foo)
		s.RespondTo(ctx, "POST /v2/test1/bar/blobs/uploads/?from=test1%2Ffoo&mount="+blob.Digest.String(),
			httptest.WithHeaders(otherRepoTokenHeaders),
			httptest.WithHeader("Content-Length", "0"),
		).ExpectStatus(t, http.StatusCreated)

		// the blob should now be visible in both repos
		expectBlobExists(t, s, tokenHeaders, "test1/foo", blob)
		expectBlobExists(t, s, otherRepoTokenHeaders, "test1/bar", blob)

		// test failure case: no delete permission
		s.RespondTo(ctx, "DELETE /v2/test1/foo/blobs/"+blob.Digest.String(),
			httptest.WithHeaders(tokenHeaders),
		).ExpectJSON(t, http.StatusUnauthorized, test.ErrorCode(keppel.ErrDenied))

		// test failure case: no such blob
		s.RespondTo(ctx, "DELETE /v2/test1/foo/blobs/"+test.DeterministicDummyDigest(1).String(),
			httptest.WithHeaders(deleteTokenHeaders),
		).ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrBlobUnknown))

		s.RespondTo(ctx, "DELETE /v2/test1/foo/blobs/thisisnotadigest",
			httptest.WithHeaders(deleteTokenHeaders),
		).ExpectJSON(t, http.StatusBadRequest, test.ErrorCode(keppel.ErrDigestInvalid))

		// we only had failed DELETEs until now, so the blob should still be there
		expectBlobExists(t, s, tokenHeaders, "test1/foo", blob)
		expectBlobExists(t, s, otherRepoTokenHeaders, "test1/bar", blob)

		// test success case: delete the blob from the first repo
		s.RespondTo(ctx, "DELETE /v2/test1/foo/blobs/"+blob.Digest.String(),
			httptest.WithHeaders(deleteTokenHeaders),
		).
			ExpectHeader(t, "Content-Length", "0").
			ExpectStatus(t, http.StatusAccepted)

		// after successful DELETE, the blob should be gone from test1/foo...
		s.RespondTo(ctx, "GET /v2/test1/foo/blobs/"+blob.Digest.String(),
			httptest.WithHeaders(tokenHeaders),
		).ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrBlobUnknown))

		// ...but still be visible in test1/bar
		expectBlobExists(t, s, otherRepoTokenHeaders, "test1/bar", blob)
	})
}

func TestCrossRepositoryBlobMount(t *testing.T) {
	ctx := t.Context()

	testWithPrimary(t, nil, func(s test.Setup) {
		readOnlyTokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull")
		tokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull,push")
		otherRepoTokenHeaders := s.GetTokenHeaders(t, "repository:test1/bar:pull,push")

		blob := test.NewBytes([]byte("just some random data"))

		// upload a blob to test1/bar so that we can test mounting it to test1/foo
		blob.MustUpload(t, s, barRepoRef)

		// failed blob mounts are usually not fatal, and instead return 202 Accepted to start a regular blob upload session
		fallbackToRegularUpload := func(resp httptest.Response) {
			resp.ExpectHeader(t, "Content-Length", "0").ExpectStatus(t, http.StatusAccepted)
		}

		// test failure cases: token does not have push access
		s.RespondTo(ctx, "POST /v2/test1/foo/blobs/uploads/?from=test1/bar&mount="+blob.Digest.String(),
			httptest.WithHeaders(readOnlyTokenHeaders),
		).ExpectJSON(t, http.StatusUnauthorized, test.ErrorCode(keppel.ErrDenied))

		// test failure cases: source repo does not exist
		s.RespondTo(ctx, "POST /v2/test1/foo/blobs/uploads/?from=test1/qux&mount="+blob.Digest.String(),
			httptest.WithHeaders(tokenHeaders),
		).Expect(fallbackToRegularUpload)

		s.RespondTo(ctx, "POST /v2/test1/foo/blobs/uploads/?from=test1/:qux&mount="+blob.Digest.String(),
			httptest.WithHeaders(tokenHeaders),
		).Expect(fallbackToRegularUpload)

		// test failure cases: cannot mount across accounts
		s.RespondTo(ctx, "POST /v2/test1/foo/blobs/uploads/?from=test2/foo&mount="+blob.Digest.String(),
			httptest.WithHeaders(tokenHeaders),
		).Expect(fallbackToRegularUpload)

		// test failure cases: digest is malformed or wrong
		s.RespondTo(ctx, "POST /v2/test1/foo/blobs/uploads/?from=test1/bar&mount=wrong",
			httptest.WithHeaders(tokenHeaders),
		).Expect(fallbackToRegularUpload)

		s.RespondTo(ctx, "POST /v2/test1/foo/blobs/uploads/?from=test1/bar&mount="+test.DeterministicDummyDigest(1).String(),
			httptest.WithHeaders(tokenHeaders),
		).Expect(fallbackToRegularUpload)

		// since these all failed, the blob should not be available in test1/foo yet
		s.RespondTo(ctx, "GET /v2/test1/foo/blobs/"+blob.Digest.String(),
			httptest.WithHeaders(tokenHeaders),
		).ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrBlobUnknown))

		// test success case
		s.RespondTo(ctx, "POST /v2/test1/foo/blobs/uploads/?from=test1/bar&mount="+blob.Digest.String(),
			httptest.WithHeaders(tokenHeaders),
		).ExpectHeaders(t, http.Header{
			"Content-Length": {"0"},
			"Location":       {"/v2/test1/foo/blobs/" + blob.Digest.String()},
		}).ExpectStatus(t, http.StatusCreated)

		// now the blob should be available in both the original and the new repo
		expectBlobExists(t, s, tokenHeaders, "test1/foo", blob)
		expectBlobExists(t, s, otherRepoTokenHeaders, "test1/bar", blob)
	})
}
