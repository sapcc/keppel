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

package registryv2_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/sapcc/go-bits/assert"

	"github.com/sapcc/keppel/internal/drivers/trivial"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

var (
	currentlyWithAnycast bool

	//only for use with .MustUpload()
	fooRepoRef = keppel.Repository{AccountName: "test1", Name: "foo"}
	barRepoRef = keppel.Repository{AccountName: "test1", Name: "bar"}
)

//the auth tenant ID that all test accounts use
const authTenantID = "test1authtenant"

func testWithPrimary(t *testing.T, rle *keppel.RateLimitEngine, action func(test.Setup)) {
	test.WithRoundTripper(func(tt *test.RoundTripper) {
		for _, withAnycast := range []bool{false, true} {
			s := test.NewSetup(t,
				test.WithAnycast(withAnycast),
				test.WithAccount(keppel.Account{Name: "test1", AuthTenantID: authTenantID}),
				test.WithQuotas,
				test.WithPeerAPI,
				test.WithRateLimitEngine(rle),
			)
			currentlyWithAnycast = withAnycast

			//run the tests for this scenario
			action(s)

			//shutdown DB to free up connections (otherwise the test eventually fails
			//with Postgres saying "too many clients already")
			err := s.DB.Db.Close()
			if err != nil {
				t.Fatal(err.Error())
			}
		}
	})
}

func testWithReplica(t *testing.T, s1 test.Setup, strategy string, action func(firstPass bool, s2 test.Setup)) {
	testAccount := keppel.Account{Name: "test1", AuthTenantID: authTenantID}
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
		_, err := s1.DB.Exec(`DELETE FROM peers`)
		if err != nil {
			t.Fatal(err.Error())
		}
		tt := http.DefaultTransport.(*test.RoundTripper) //nolint:errcheck
		tt.Handlers["registry-secondary.example.org"] = nil
	}()

	//run the testcase once with the primary registry available
	t.Logf("running first pass for strategy %s", strategy)
	action(true, s)
	if t.Failed() {
		t.FailNow()
	}

	//sever the network connection to the primary registry and re-run all testcases
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

//To be called inside testWithReplica() if the test is specifically about
//testing how anycast requests are redirected between peers.
func testAnycast(t *testing.T, firstPass bool, db2 *keppel.DB, action func()) {
	t.Helper()

	//the second pass of testWithReplica() has a severed network connection, so anycast is not possible
	if !firstPass {
		return
	}
	//to make sure that we actually anycast, the replica must not have the "test1" account
	_, err := db2.Exec(`DELETE FROM accounts`)
	if err != nil {
		t.Fatal(err.Error())
	}

	action()
}

////////////////////////////////////////////////////////////////////////////////
// helpers for setting up test scenarios

func getBlobUpload(t *testing.T, h http.Handler, token, fullRepoName string) (uploadURL, uploadUUID string) {
	t.Helper()
	resp, _ := assert.HTTPRequest{
		Method:       "POST",
		Path:         fmt.Sprintf("/v2/%s/blobs/uploads/", fullRepoName),
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

//nolint:unparam
func getBlobUploadURL(t *testing.T, h http.Handler, token, fullRepoName string) string {
	t.Helper()
	u, _ := getBlobUpload(t, h, token, fullRepoName)
	return u
}

////////////////////////////////////////////////////////////////////////////////
// reusable assertions

func expectBlobExists(t *testing.T, h http.Handler, token, fullRepoName string, blob test.Bytes, additionalHeaders map[string]string) {
	t.Helper()
	for _, method := range []string{"GET", "HEAD"} {
		respBody := blob.Contents
		if method == "HEAD" {
			respBody = nil
		}
		req := assert.HTTPRequest{
			Method:       method,
			Path:         "/v2/" + fullRepoName + "/blobs/" + blob.Digest.String(),
			Header:       map[string]string{"Authorization": "Bearer " + token},
			ExpectStatus: http.StatusOK,
			ExpectHeader: map[string]string{
				test.VersionHeaderKey:   test.VersionHeaderValue,
				"Content-Length":        strconv.Itoa(len(blob.Contents)),
				"Content-Type":          "application/octet-stream",
				"Docker-Content-Digest": blob.Digest.String(),
			},
			ExpectBody: assert.ByteData(respBody),
		}
		for k, v := range additionalHeaders {
			req.Header[k] = v
		}
		req.Check(t, h)
	}
}

//nolint:unparam
func expectManifestExists(t *testing.T, h http.Handler, token, fullRepoName string, manifest test.Bytes, reference string, additionalHeaders map[string]string) {
	t.Helper()
	for _, method := range []string{"GET", "HEAD"} {
		respBody := manifest.Contents
		if method == "HEAD" {
			respBody = nil
		}
		if reference == "" {
			reference = manifest.Digest.String()
		}

		req := assert.HTTPRequest{
			Method:       method,
			Path:         fmt.Sprintf("/v2/%s/manifests/%s", fullRepoName, reference),
			Header:       map[string]string{"Authorization": "Bearer " + token},
			ExpectStatus: http.StatusOK,
			ExpectHeader: map[string]string{
				test.VersionHeaderKey:   test.VersionHeaderValue,
				"Content-Type":          manifest.MediaType,
				"Docker-Content-Digest": manifest.Digest.String(),
			},
			ExpectBody: assert.ByteData(respBody),
		}
		for k, v := range additionalHeaders {
			req.Header[k] = v
		}

		//without Accept header
		req.Check(t, h)

		//with matching Accept header
		req.Header["Accept"] = manifest.MediaType
		req.Check(t, h)

		//with mismatching Accept header
		req.Header["Accept"] = "text/plain"
		req.ExpectStatus = http.StatusNotFound
		req.ExpectHeader = test.VersionHeader
		if method == "GET" {
			req.ExpectBody = test.ErrorCode(keppel.ErrManifestUnknown)
		}
		req.Check(t, h)
	}
}

func expectStorageEmpty(t *testing.T, sd *trivial.StorageDriver, db *keppel.DB) {
	t.Helper()
	//test that no blobs were yet committed to the DB...
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

//nolint:unparam
func testWithAccountInMaintenance(t *testing.T, db *keppel.DB, accountName string, action func()) {
	_, err := db.Exec("UPDATE accounts SET in_maintenance = TRUE WHERE name = $1", accountName)
	if err != nil {
		t.Fatal(err.Error())
	}
	action()
	_, err = db.Exec("UPDATE accounts SET in_maintenance = FALSE WHERE name = $1", accountName)
	if err != nil {
		t.Fatal(err.Error())
	}
}

////////////////////////////////////////////////////////////////////////////////

func sha256Of(data []byte) string {
	sha256Hash := sha256.Sum256(data)
	return hex.EncodeToString(sha256Hash[:])
}
