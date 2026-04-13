// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"testing"

	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/httptest"
	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/drivers/trivial"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestMain(m *testing.M) {
	easypg.WithTestDB(m, func() int { return m.Run() })
}

var (
	currentlyWithAnycast bool

	// only for use with .MustUpload()
	fooRepoRef = models.Repository{AccountName: "test1", Name: "foo"}
	barRepoRef = models.Repository{AccountName: "test1", Name: "bar"}
)

// the auth tenant ID that all test accounts use
const authTenantID = "test1authtenant"

func testWithPrimary(t *testing.T, setupOptions []test.SetupOption, action func(test.Setup)) {
	test.WithRoundTripper(func(tt *test.RoundTripper) {
		for _, withAnycast := range []bool{false, true} {
			opts := append(slices.Clone(setupOptions),
				test.WithAnycast(withAnycast),
				test.WithAccount(models.Account{Name: "test1", AuthTenantID: authTenantID}),
				test.WithQuotas,
			)
			s := test.NewSetup(t, opts...)
			currentlyWithAnycast = withAnycast

			// run the tests for this scenario
			action(s)

			// shutdown DB to free up connections (otherwise the test eventually fails
			// with Postgres saying "too many clients already")
			must.SucceedT(t, s.DB.Db.Close())
		}
	})
}

func testWithReplica(t *testing.T, s1 test.Setup, strategy string, action func(firstPass bool, s2 test.Setup)) {
	testAccount := models.Account{Name: "test1", AuthTenantID: authTenantID}
	switch strategy {
	case "on_first_use":
		testAccount.UpstreamPeerHostName = "registry.example.org"
	case "from_external_on_first_use":
		testAccount.ExternalPeerURL = "registry.example.org/test1"
		testAccount.ExternalPeerUserName = "replication@registry-secondary.example.org"
		testAccount.ExternalPeerPassword = test.GetReplicationPassword()
	default:
		t.Fatalf("unknown strategy: %q", strategy)
	}

	s := test.NewSetup(t,
		test.IsSecondaryTo(&s1),
		test.WithAnycast(currentlyWithAnycast),
		test.WithAccount(testAccount),
		test.WithQuotas,
		test.WithPeerAPI,
	)

	defer func() {
		test.MustExec(t, s1.DB, `DELETE FROM peers`)
		tt := http.DefaultTransport.(*test.RoundTripper)
		tt.Handlers["registry-secondary.example.org"] = nil
	}()

	// run the testcase once with the primary registry available
	t.Logf("running first pass for strategy %s", strategy)
	action(true, s)
	if t.Failed() {
		t.FailNow()
	}

	// sever the network connection to the primary registry and re-run all testcases
	t.Logf("running second pass for strategy %s", strategy)
	test.WithoutRoundTripper(func() {
		action(false, s)
	})
	if t.Failed() {
		t.FailNow()
	}
}

func testWithAllReplicaTypes(t *testing.T, s1 test.Setup, action func(strategy string, firstPass bool, s test.Setup)) {
	for _, strategy := range []string{"on_first_use", "from_external_on_first_use"} {
		testWithReplica(t, s1, strategy, func(firstPass bool, s2 test.Setup) {
			action(strategy, firstPass, s2)
		})
	}
}

// To be called inside testWithReplica() if the test is specifically about
// testing how anycast requests are redirected between peers.
func testAnycast(t *testing.T, firstPass bool, db2 *keppel.DB, action func()) {
	t.Helper()

	// the second pass of testWithReplica() has a severed network connection, so anycast is not possible
	if !firstPass {
		return
	}
	// to make sure that we actually anycast, the replica must not have the "test1" account
	test.MustExec(t, db2, `DELETE FROM accounts`)

	action()
}

////////////////////////////////////////////////////////////////////////////////
// helpers for setting up test scenarios

func getBlobUpload(t *testing.T, h httptest.Handler, hdr http.Header, fullRepoName string) (uploadURL, uploadUUID string) {
	t.Helper()

	h.RespondTo(t.Context(),
		fmt.Sprintf("POST /v2/%s/blobs/uploads/", fullRepoName),
		httptest.WithHeaders(hdr),
	).ExpectHeaders(t, http.Header{
		test.VersionHeaderKey: {test.VersionHeaderValue},
		"Content-Length":      {"0"},
		"Range":               {"0-0"},
	}).
		CaptureHeader("Location", &uploadURL).
		CaptureHeader("Blob-Upload-Session-Id", &uploadUUID).
		ExpectStatus(t, http.StatusAccepted)

	return
}

//nolint:unparam
func getBlobUploadURL(t *testing.T, h httptest.Handler, hdr http.Header, fullRepoName string) string {
	t.Helper()
	u, _ := getBlobUpload(t, h, hdr, fullRepoName)
	return u
}

////////////////////////////////////////////////////////////////////////////////
// reusable assertions

func bodyForMethod(method string, body []byte) []byte {
	if method == "HEAD" {
		return nil
	}
	return body
}

func expectBlobExists(t *testing.T, h httptest.Handler, hdr http.Header, fullRepoName string, blob test.Bytes) {
	t.Helper()
	for _, method := range []string{"GET", "HEAD"} {
		h.RespondTo(t.Context(),
			fmt.Sprintf("%s /v2/%s/blobs/%s", method, fullRepoName, blob.Digest.String()),
			httptest.WithHeaders(hdr),
		).ExpectHeaders(t, http.Header{
			test.VersionHeaderKey:   {test.VersionHeaderValue},
			"Content-Length":        {strconv.Itoa(len(blob.Contents))},
			"Content-Type":          {blob.MediaType},
			"Docker-Content-Digest": {blob.Digest.String()},
		}).ExpectBody(t, http.StatusOK, bodyForMethod(method, blob.Contents))
	}
}

//nolint:unparam
func expectManifestExists(t *testing.T, h httptest.Handler, hdr http.Header, fullRepoName string, manifest test.Bytes, reference string) {
	t.Helper()
	for _, method := range []string{"GET", "HEAD"} {
		// NOTE: `hdr.Get("Accept")` may be empty, in which case we test without any non-empty Accept header
		for _, acceptHeader := range []string{hdr.Get("Accept"), manifest.MediaType, "text/plain"} {
			resp := h.RespondTo(t.Context(),
				fmt.Sprintf("%s /v2/%s/manifests/%s", method, fullRepoName, cmp.Or(reference, manifest.Digest.String())),
				httptest.WithHeaders(hdr),
				httptest.WithHeader("Accept", acceptHeader), // must be last to take priority over hdr["Accept"] (if that is set)
			).ExpectHeader(t, test.VersionHeaderKey, test.VersionHeaderValue)

			if acceptHeader == "text/plain" {
				// with mismatching Accept header, expect error response
				if method == "GET" {
					resp.ExpectJSON(t, http.StatusNotAcceptable, test.ErrorCode(keppel.ErrManifestUnknown))
				} else {
					resp.ExpectStatus(t, http.StatusNotAcceptable)
				}
			} else {
				// with no Accept header or matching Accept header, expect successful response
				resp.ExpectHeaders(t, http.Header{
					"Content-Type":          {manifest.MediaType},
					"Docker-Content-Digest": {manifest.Digest.String()},
				}).ExpectBody(t, http.StatusOK, bodyForMethod(method, manifest.Contents))
			}
		}
	}
}

func expectStorageEmpty(t *testing.T, sd *trivial.StorageDriver, db *keppel.DB) {
	t.Helper()
	// test that no blobs were yet committed to the DB...
	count := must.ReturnT(db.SelectInt(`SELECT COUNT(*) FROM blobs`))(t)
	if count > 0 {
		t.Errorf("expected 0 blobs in the DB, but found %d blobs", count)
	}

	// ...nor to the storage
	if sd.BlobCount() > 0 {
		t.Errorf("expected 0 blobs in the storage, but found %d blobs", sd.BlobCount())
	}

	// also there should be no unfinished uploads
	count = must.ReturnT(db.SelectInt(`SELECT COUNT(*) FROM uploads`))(t)
	if count > 0 {
		t.Errorf("expected 0 uploads in the DB, but found %d uploads", count)
	}
}

//nolint:unparam
func testWithAccountIsDeleting(t *testing.T, db *keppel.DB, accountName models.AccountName, action func()) {
	_ = must.ReturnT(db.Exec("UPDATE accounts SET is_deleting = TRUE WHERE name = $1", accountName))(t)
	action()
	_ = must.ReturnT(db.Exec("UPDATE accounts SET is_deleting = FALSE WHERE name = $1", accountName))(t)
}
