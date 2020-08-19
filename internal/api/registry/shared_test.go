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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/keppel/internal/api"
	authapi "github.com/sapcc/keppel/internal/api/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
	"golang.org/x/crypto/bcrypt"
)

var (
	//these credentials are in global vars so that we don't have to recompute them
	//in every test run (bcrypt is intentionally CPU-intensive)
	replicationPassword     string
	replicationPasswordHash string

	scenarios = []test.SetupOptions{
		{WithAnycast: false},
		{WithAnycast: true},
	}
	currentScenario test.SetupOptions
)

func testWithPrimary(t *testing.T, rle *keppel.RateLimitEngine, action func(http.Handler, keppel.Configuration, *keppel.DB, *test.AuthDriver, *test.StorageDriver, *test.FederationDriver, *test.Clock)) {
	for _, scenario := range scenarios {
		currentScenario = scenario
		cfg, db := test.Setup(t, &scenario)

		//set up a dummy account for testing
		testAccount := keppel.Account{
			Name:         "test1",
			AuthTenantID: "test1authtenant",
		}
		err := db.Insert(&testAccount)
		if err != nil {
			t.Fatal(err.Error())
		}

		//setup ample quota for all tests
		err = db.Insert(&keppel.Quotas{
			AuthTenantID:  "test1authtenant",
			ManifestCount: 100,
		})
		if err != nil {
			t.Fatal(err.Error())
		}

		//setup a fleet of drivers
		ad, err := keppel.NewAuthDriver("unittest", nil)
		if err != nil {
			t.Fatal(err.Error())
		}
		sd, err := keppel.NewStorageDriver("in-memory-for-testing", ad, cfg)
		if err != nil {
			t.Fatal(err.Error())
		}
		fd, err := keppel.NewFederationDriver("unittest", ad, cfg)
		if err != nil {
			t.Fatal(err.Error())
		}
		fd.RecordExistingAccount(testAccount, time.Unix(0, 0))

		//wire up the HTTP APIs
		clock := &test.Clock{}
		sidGen := &test.StorageIDGenerator{}
		h := api.Compose(
			NewAPI(cfg, fd, sd, db, rle).OverrideTimeNow(clock.Now).OverrideGenerateStorageID(sidGen.Next),
			authapi.NewAPI(cfg, ad, fd, db),
		)

		//run the tests for this scenario
		action(h, cfg, db, ad.(*test.AuthDriver), sd.(*test.StorageDriver), fd.(*test.FederationDriver), clock)

		//shutdown DB to free up connections (otherwise the test eventually fails
		//with Postgres saying "too many clients already")
		err = db.Db.Close()
		if err != nil {
			t.Fatal(err.Error())
		}
	}
}

func testWithReplica(t *testing.T, h1 http.Handler, db1 *keppel.DB, clock *test.Clock, action func(bool, http.Handler, keppel.Configuration, *keppel.DB, *test.AuthDriver, *test.StorageDriver)) {
	opts := currentScenario
	opts.IsSecondary = true
	cfg2, db2 := test.Setup(t, &opts)

	//give the secondary registry credentials for replicating from the primary
	if replicationPassword == "" {
		//this password needs to be constant because it appears in some fixtures/*.sql
		replicationPassword = "a4cb6fae5b8bb91b0b993486937103dab05eca93"

		hashBytes, _ := bcrypt.GenerateFromPassword([]byte(replicationPassword), 8)
		replicationPasswordHash = string(hashBytes)
	}

	err := db2.Insert(&keppel.Peer{
		HostName:    "registry.example.org",
		OurPassword: replicationPassword,
	})
	if err != nil {
		t.Fatal(err.Error())
	}
	err = db1.Insert(&keppel.Peer{
		HostName:                 "registry-secondary.example.org",
		TheirCurrentPasswordHash: replicationPasswordHash,
	})
	if err != nil {
		t.Fatal(err.Error())
	}
	defer func() {
		_, err := db1.Exec(`DELETE FROM peers`)
		if err != nil {
			t.Fatal(err.Error())
		}
	}()

	//set up a dummy account for testing
	testAccount := keppel.Account{
		Name:                 "test1",
		AuthTenantID:         "test1authtenant",
		UpstreamPeerHostName: "registry.example.org",
	}
	err = db2.Insert(&testAccount)
	if err != nil {
		t.Fatal(err.Error())
	}

	//setup ample quota for all tests
	err = db2.Insert(&keppel.Quotas{
		AuthTenantID:  "test1authtenant",
		ManifestCount: 100,
	})
	if err != nil {
		t.Fatal(err.Error())
	}

	//setup a fleet of drivers for keppel-secondary
	ad2, err := keppel.NewAuthDriver("unittest", nil)
	if err != nil {
		t.Fatal(err.Error())
	}
	fd2, err := keppel.NewFederationDriver("unittest", ad2, cfg2)
	if err != nil {
		t.Fatal(err.Error())
	}
	sd2, err := keppel.NewStorageDriver("in-memory-for-testing", ad2, cfg2)
	if err != nil {
		t.Fatal(err.Error())
	}
	fd2.RecordExistingAccount(testAccount, time.Unix(0, 0))

	sidGen := &test.StorageIDGenerator{}
	h2 := api.Compose(
		NewAPI(cfg2, fd2, sd2, db2, nil).OverrideTimeNow(clock.Now).OverrideGenerateStorageID(sidGen.Next),
		authapi.NewAPI(cfg2, ad2, fd2, db2),
	)

	//the secondary registry wants to talk to the primary registry over HTTPS, so
	//attach the primary registry's HTTP handler to the http.DefaultClient
	tt := &test.RoundTripper{
		Handlers: map[string]http.Handler{
			"registry.example.org":           h1,
			"registry-secondary.example.org": h2,
		},
	}
	http.DefaultClient.Transport = tt
	defer func() {
		http.DefaultClient.Transport = nil
	}()

	//run the testcase once with the primary registry available
	action(true, h2, cfg2, db2, ad2.(*test.AuthDriver), sd2.(*test.StorageDriver))
	if t.Failed() {
		t.FailNow()
	}

	//sever the network connection to the primary registry and re-run all testcases
	http.DefaultClient.Transport = nil
	action(false, h2, cfg2, db2, ad2.(*test.AuthDriver), sd2.(*test.StorageDriver))
	if t.Failed() {
		t.FailNow()
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

func getToken(t *testing.T, h http.Handler, ad keppel.AuthDriver, scope string, perms ...keppel.Permission) string {
	t.Helper()

	return ad.(*test.AuthDriver).GetTokenForTest(t, h, "registry.example.org", scope, "test1authtenant", perms...)
}

func getTokenForSecondary(t *testing.T, h http.Handler, ad keppel.AuthDriver, scope string, perms ...keppel.Permission) string {
	t.Helper()

	return ad.(*test.AuthDriver).GetTokenForTest(t, h, "registry-secondary.example.org", scope, "test1authtenant", perms...)
}

func getTokenForAnycast(t *testing.T, h http.Handler, ad keppel.AuthDriver, scope string, perms ...keppel.Permission) string {
	t.Helper()

	return ad.(*test.AuthDriver).GetTokenForTest(t, h, "registry-global.example.org", scope, "test1authtenant", perms...)
}

func uploadBlob(t *testing.T, h http.Handler, token, fullRepoName string, blob test.Bytes) {
	t.Helper()
	assert.HTTPRequest{
		Method: "POST",
		Path:   fmt.Sprintf("/v2/%s/blobs/uploads/?digest=%s", fullRepoName, blob.Digest),
		Header: map[string]string{
			"Authorization":  "Bearer " + token,
			"Content-Length": strconv.Itoa(len(blob.Contents)),
			"Content-Type":   "application/octet-stream",
		},
		Body:         assert.ByteData(blob.Contents),
		ExpectStatus: http.StatusCreated,
	}.Check(t, h)
}

func uploadManifest(t *testing.T, h http.Handler, token, fullRepoName string, manifest test.Bytes, reference string) {
	t.Helper()
	if reference == "" {
		reference = manifest.Digest.String()
	}
	assert.HTTPRequest{
		Method: "PUT",
		Path:   fmt.Sprintf("/v2/%s/manifests/%s", fullRepoName, reference),
		Header: map[string]string{
			"Authorization": "Bearer " + token,
			"Content-Type":  manifest.MediaType,
		},
		Body:         assert.ByteData(manifest.Contents),
		ExpectStatus: http.StatusCreated,
	}.Check(t, h)
}

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
			Path:         "/v2/test1/foo/manifests/" + reference,
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
