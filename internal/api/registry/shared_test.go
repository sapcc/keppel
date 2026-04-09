// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"testing"

	"github.com/sapcc/go-bits/assert"
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

func getBlobUpload(t *testing.T, ctx context.Context, h httptest.Handler, token, fullRepoName string) (uploadURL, uploadUUID string) {
	t.Helper()
	resp := h.RespondTo(ctx, "POST "+fmt.Sprintf("/v2/%s/blobs/uploads/", fullRepoName),
		httptest.WithHeader("Authorization", "Bearer "+token))
	resp.ExpectStatus(t, http.StatusAccepted)
	assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
	assert.Equal(t, resp.Header().Get("Content-Length"), "0")
	assert.Equal(t, resp.Header().Get("Range"), "0-0")
	return resp.Header().Get("Location"), resp.Header().Get("Blob-Upload-Session-Id")
}

//nolint:unparam
func getBlobUploadURL(t *testing.T, ctx context.Context, h httptest.Handler, token, fullRepoName string) string {
	t.Helper()
	u, _ := getBlobUpload(t, ctx, h, token, fullRepoName)
	return u
}

////////////////////////////////////////////////////////////////////////////////
// reusable assertions

func expectBlobExists(t *testing.T, ctx context.Context, h httptest.Handler, token, fullRepoName string, blob test.Bytes, additionalHeaders map[string]string) {
	t.Helper()
	for _, method := range []string{"GET", "HEAD"} {
		opts := []httptest.RequestOption{httptest.WithHeader("Authorization", "Bearer "+token)}
		for k, v := range additionalHeaders {
			opts = append(opts, httptest.WithHeader(k, v))
		}
		resp := h.RespondTo(ctx, method+" /v2/"+fullRepoName+"/blobs/"+blob.Digest.String(), opts...)
		resp.ExpectStatus(t, http.StatusOK)
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
		assert.Equal(t, resp.Header().Get("Content-Length"), strconv.Itoa(len(blob.Contents)))
		assert.Equal(t, resp.Header().Get("Content-Type"), blob.MediaType)
		assert.Equal(t, resp.Header().Get("Docker-Content-Digest"), blob.Digest.String())
		if method == "GET" {
			assert.DeepEqual(t, "body", resp.BodyBytes(), blob.Contents)
		}
	}
}

//nolint:unparam
func expectManifestExists(t *testing.T, ctx context.Context, h httptest.Handler, token, fullRepoName string, manifest test.Bytes, reference string, additionalHeaders map[string]string) {
	t.Helper()
	if reference == "" {
		reference = manifest.Digest.String()
	}
	path := fmt.Sprintf("/v2/%s/manifests/%s", fullRepoName, reference)

	for _, method := range []string{"GET", "HEAD"} {
		baseOpts := []httptest.RequestOption{httptest.WithHeader("Authorization", "Bearer "+token)}
		for k, v := range additionalHeaders {
			baseOpts = append(baseOpts, httptest.WithHeader(k, v))
		}

		// without Accept header
		resp := h.RespondTo(ctx, method+" "+path, baseOpts...)
		resp.ExpectStatus(t, http.StatusOK)
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
		assert.Equal(t, resp.Header().Get("Content-Type"), manifest.MediaType)
		assert.Equal(t, resp.Header().Get("Docker-Content-Digest"), manifest.Digest.String())
		if method == "GET" {
			assert.DeepEqual(t, "body", resp.BodyBytes(), manifest.Contents)
		}

		// with matching Accept header
		resp = h.RespondTo(ctx, method+" "+path, append(baseOpts, httptest.WithHeader("Accept", manifest.MediaType))...)
		resp.ExpectStatus(t, http.StatusOK)
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
		assert.Equal(t, resp.Header().Get("Content-Type"), manifest.MediaType)
		assert.Equal(t, resp.Header().Get("Docker-Content-Digest"), manifest.Digest.String())
		if method == "GET" {
			assert.DeepEqual(t, "body", resp.BodyBytes(), manifest.Contents)
		}

		// with mismatching Accept header
		resp = h.RespondTo(ctx, method+" "+path, append(baseOpts, httptest.WithHeader("Accept", "text/plain"))...)
		if method == "GET" {
			resp.ExpectJSON(t, http.StatusNotAcceptable, test.ErrorCode(keppel.ErrManifestUnknown))
		} else {
			resp.ExpectStatus(t, http.StatusNotAcceptable)
		}
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
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
