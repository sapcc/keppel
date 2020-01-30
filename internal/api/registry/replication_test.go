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
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/assert"
	authapi "github.com/sapcc/keppel/internal/api/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/orchestration"
	"github.com/sapcc/keppel/internal/orchestration/localprocesses"
	"github.com/sapcc/keppel/internal/test"
	"golang.org/x/crypto/bcrypt"
)

//NOTE: This test is run from TestProxyAPI to reuse its existing primary
//registry and pushed blobs/manifests.
func testReplicationOnFirstUse(t *testing.T, hPrimary http.Handler, dbPrimary *keppel.DB, firstManifestDigest, firstBlobDigest, secondManifestDigest, secondManifestTag string) {
	cfg2, db2 := test.SetupSecondary(t)

	//give the secondary registry credentials for replicating from the primary
	replicationPasswordBytes := make([]byte, 20)
	_, err := rand.Read(replicationPasswordBytes)
	if err != nil {
		t.Fatal(err.Error())
	}

	replicationPassword := hex.EncodeToString(replicationPasswordBytes)
	err = db2.Insert(&keppel.Peer{
		HostName:    "registry.example.org",
		OurPassword: replicationPassword,
	})
	if err != nil {
		t.Fatal(err.Error())
	}

	replicationPasswordHash, _ := bcrypt.GenerateFromPassword([]byte(replicationPassword), 8)
	err = dbPrimary.Insert(&keppel.Peer{
		HostName:                 "registry-secondary.example.org",
		TheirCurrentPasswordHash: string(replicationPasswordHash),
	})
	if err != nil {
		t.Fatal(err.Error())
	}

	defer func() {
		//reset primary's DB into its previous state
		_, err = dbPrimary.Exec(`DELETE FROM peers WHERE hostname = $1`,
			"registry-secondary.example.org",
		)
		if err != nil {
			t.Error(err.Error())
		}
	}()

	//set up a replicated account referencing the primary test account from TestProxyAPI
	err = db2.Insert(&keppel.Account{
		Name:                 "test1",
		AuthTenantID:         "test1authtenant",
		RegistryHTTPSecret:   "topsecret",
		UpstreamPeerHostName: "registry.example.org",
	})
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
	ad2, err := keppel.NewAuthDriver("unittest")
	if err != nil {
		t.Fatal(err.Error())
	}
	sd2, err := keppel.NewStorageDriver("in-memory-for-testing", ad2, cfg2)
	if err != nil {
		t.Fatal(err.Error())
	}
	od2, err := keppel.NewOrchestrationDriver("local-processes", sd2, cfg2, db2)
	if err != nil {
		t.Fatal(err.Error())
	}
	//avoid port collision between primary and secondary registry fleet
	od2.(*orchestration.Engine).Launcher.(*localprocesses.Driver).NextListenPort += 1000

	//run the orchestration driver's mainloop for the duration of the test
	//(the wait group is important to ensure that od.Run() runs to completion;
	//otherwise the test harness appears to kill its goroutine too early)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	defer func() {
		cancel() //uses `defer` because t.Fatal() might exit early
		wg.Wait()
	}()
	go func() {
		wg.Add(1)
		defer wg.Done()
		ok := od2.Run(ctx)
		if !ok {
			t.Error("secondary orchestration driver mainloop exited unsuccessfully")
		}
	}()

	r := mux.NewRouter()
	NewAPI(context.Background(), cfg2, od2, db2).AddTo(r)
	authapi.NewAPI(cfg2, ad2, db2).AddTo(r)

	//the secondary registry wants to talk to the primary registry over HTTPS, so
	//attach the primary registry's HTTP handler to the http.DefaultClient
	tt := &httpTransportForTest{
		Handlers: map[string]http.Handler{
			"registry.example.org":           hPrimary,
			"registry-secondary.example.org": r,
		},
	}
	http.DefaultClient.Transport = tt
	defer func() {
		http.DefaultClient.Transport = nil
	}()

	//run all replication-on-first-use (ROFU) tests once
	testROFUNonReplicatingCases(t, r, ad2, od2, db2, firstBlobDigest)
	testROFUSuccessCases(t, r, ad2, firstManifestDigest, firstBlobDigest, secondManifestDigest, secondManifestTag)
	testROFUMissingEntities(t, r, ad2)
	testROFUForbidDirectUpload(t, r, ad2)

	//wait until all async replications are done
	for idx := 0; idx < 10; idx++ {
		count, err := db2.SelectInt(`SELECT COUNT(*) FROM pending_manifests`)
		if err != nil {
			t.Fatal(err.Error())
		}
		if count == 0 {
			break
		}
		time.Sleep(50 * time.Second)
	}

	//run the positive tests again with the network connection to the primary
	//registry severed, to validate that contents have actually been replicated
	http.DefaultClient.Transport = nil
	testROFUSuccessCases(t, r, ad2, firstManifestDigest, firstBlobDigest, secondManifestDigest, secondManifestTag)
	testROFUForbidDirectUpload(t, r, ad2)
}

func testROFUNonReplicatingCases(t *testing.T, h http.Handler, ad keppel.AuthDriver, od keppel.OrchestrationDriver, db *keppel.DB, firstBlobDigest string) {
	//before replication, do a HEAD on a blob - this should only be proxied to
	//upstream and not cause a full replication (we reserve the full replication
	//for the first GET on the blob since we can then also stream the blob
	//contents to that client directly)
	token := getTokenForSecondary(t, h, ad, "repository:test1/foo:pull",
		keppel.CanPullFromAccount)
	assert.HTTPRequest{
		Method:       "HEAD",
		Path:         "/v2/test1/foo/blobs/" + firstBlobDigest,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: test.VersionHeader,
	}.Check(t, h)

	//query the backing keppel-registry to check that the blob was not actually replicated
	account, err := db.FindAccount("test1")
	if err != nil {
		t.Fatal(err.Error())
	}
	req, _ := http.NewRequest("GET", "/v2/test1/foo/blobs/"+firstBlobDigest, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := od.DoHTTPRequest(*account, req, keppel.FollowRedirects)
	if err != nil {
		t.Fatal(err.Error())
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected blob %s to be 404 in secondary keppel-registry, but is actually %s", firstBlobDigest, resp.Status)
	}
}

func testROFUSuccessCases(t *testing.T, h http.Handler, ad keppel.AuthDriver, firstManifestDigest, firstBlobDigest, secondManifestDigest, secondManifestTag string) {
	//pull a blob that exists upstream, but not locally yet - this will
	//transparently fetch the blob into the local registry
	token := getTokenForSecondary(t, h, ad, "repository:test1/foo:pull",
		keppel.CanPullFromAccount)
	_, blobData := assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/blobs/" + firstBlobDigest,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: test.VersionHeader,
	}.Check(t, h)
	assertDigest(t, "blob", blobData, firstBlobDigest)

	//pull a manifest referencing that blob that exists upstream, but not locally
	//yet - this will transparently fetch the manifest into the local registry
	_, manifestData := assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/manifests/" + firstManifestDigest,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Content-Type":        "application/vnd.docker.distribution.manifest.v2+json",
		},
	}.Check(t, h)
	assertDigest(t, "manifest", manifestData, firstManifestDigest)

	//pull a second manifest - this differs from the previous test case in two ways:
	//1. the pull happens by tag, not by manifest digest
	//2. the blob referenced in the manifest is not pulled beforehand and thus
	//will be replicated during this request
	_, manifestData = assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/manifests/" + secondManifestTag,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Content-Type":        "application/vnd.docker.distribution.manifest.v2+json",
		},
	}.Check(t, h)
	assertDigest(t, "manifest", manifestData, secondManifestDigest)
}

func assertDigest(t *testing.T, objectType string, data []byte, expectedDigest string) {
	t.Helper()
	hash := sha256.Sum256(data)
	assert.DeepEqual(t, objectType+" digest",
		"sha256:"+hex.EncodeToString(hash[:]),
		expectedDigest,
	)
}

func testROFUMissingEntities(t *testing.T, h http.Handler, ad keppel.AuthDriver) {
	//try to pull a manifest by tag that exists neither locally nor upstream
	token := getTokenForSecondary(t, h, ad, "repository:test1/foo:pull",
		keppel.CanPullFromAccount)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/manifests/thisdoesnotexist",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusNotFound,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrManifestUnknown),
	}.Check(t, h)

	//try to pull a manifest by hash that exists neither locally nor upstream
	bogusDigest := "sha256:" + strings.Repeat("0", 64)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/manifests/" + bogusDigest,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusNotFound,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrManifestUnknown),
	}.Check(t, h)

	//try to pull a blob that exists neither locally nor upstream
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/blobs/" + bogusDigest,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusNotFound,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrBlobUnknown),
	}.Check(t, h)
}

func testROFUForbidDirectUpload(t *testing.T, h http.Handler, ad keppel.AuthDriver) {
	token := getTokenForSecondary(t, h, ad, "repository:test1/foo:pull,push",
		keppel.CanPullFromAccount, keppel.CanPushToAccount)

	deniedMessage := test.ErrorCodeWithMessage{
		Code:    keppel.ErrDenied,
		Message: "cannot push into replica account (push to registry.example.org/test1/foo instead!)",
	}

	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/v2/test1/foo/blobs/uploads/",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusForbidden,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   deniedMessage,
	}.Check(t, h)

	assert.HTTPRequest{
		Method:       "PUT",
		Path:         "/v2/test1/foo/manifests/anotherone",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		Body:         assert.StringData("request body does not matter"),
		ExpectStatus: http.StatusForbidden,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   deniedMessage,
	}.Check(t, h)
}
