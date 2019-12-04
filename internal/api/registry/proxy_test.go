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
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	authapi "github.com/sapcc/keppel/internal/api/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/orchestration"
	_ "github.com/sapcc/keppel/internal/orchestration/localprocesses"
	"github.com/sapcc/keppel/internal/test"
)

//It turns out that starting up a registry takes surprisingly long, so this
//test bundles as many testcases as possible in one run to reduce the waiting.
func TestProxyAPI(t *testing.T) {
	cfg, db := test.Setup(t)

	//set up a dummy account for testing
	err := db.Insert(&keppel.Account{
		Name:               "test1",
		AuthTenantID:       "test1authtenant",
		RegistryHTTPSecret: "topsecret",
	})
	if err != nil {
		t.Fatal(err.Error())
	}

	//setup a fleet of drivers
	ad, err := keppel.NewAuthDriver("unittest")
	if err != nil {
		t.Fatal(err.Error())
	}
	sd, err := keppel.NewStorageDriver("in-memory-for-testing", ad, cfg)
	if err != nil {
		t.Fatal(err.Error())
	}
	od, err := keppel.NewOrchestrationDriver("local-processes", sd, cfg, db)
	if err != nil {
		t.Fatal(err.Error())
	}

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
		ok := od.Run(ctx)
		if !ok {
			t.Error("orchestration driver mainloop exited unsuccessfully")
		}
	}()

	//run the API testcases
	clock := &test.Clock{}
	r := mux.NewRouter()
	NewAPI(cfg, od, db).OverrideTimeNow(clock.Now).AddTo(r)
	authapi.NewAPI(cfg, ad, db).AddTo(r)

	clock.Step()
	testVersionCheckEndpoint(t, r, ad)
	clock.Step()
	testPullNonExistentTag(t, r, ad)
	clock.Step()
	testPushAndPull(t, r, ad, db,
		"fixtures/example-docker-image-config.json",
		"fixtures/001-before-push.sql",
		"fixtures/002-after-push.sql",
	)
	clock.Step()
	testPushAndPull(t, r, ad, db,
		"fixtures/example-docker-image-config2.json",
		"fixtures/002-after-push.sql",
		"fixtures/003-after-second-push.sql",
	)
	clock.Step()
	testPullExistingNotAllowed(t, r, ad)
	clock.Step()
	testDeleteManifest(t, r, ad, db,
		//the first manifest, which is not referenced by tags anymore
		"sha256:86fa8722ca7f27e97e1bc5060c3f6720bf43840f143f813fcbe48ed4cbeebb90",
		//like 003, but without that manifest
		"fixtures/004-after-first-delete.sql",
	)
	clock.Step()
	testDeleteManifest(t, r, ad, db,
		//the second manifest, which is referenced by the "latest" tag
		"sha256:65147aad93781ff7377b8fb81dab153bd58ffe05b5dc00b67b3035fa9420d2de",
		//no tags or manifests left, but repo is left over
		"fixtures/005-after-second-delete.sql",
	)
	clock.Step()

	//run some additional testcases for the orchestration engine
	testKillAndRestartRegistry(t, r, ad, od)
}

func testVersionCheckEndpoint(t *testing.T, h http.Handler, ad keppel.AuthDriver) {
	//without token, expect auth challenge
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/",
		ExpectStatus: http.StatusUnauthorized,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Www-Authenticate":    `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org"`,
		},
		ExpectBody: assert.JSONObject{
			"errors": []assert.JSONObject{{
				"code":    keppel.ErrUnauthorized,
				"message": "authentication required",
				"detail":  "no bearer token found in request headers",
			}},
		},
	}.Check(t, h)

	//with token, expect status code 200
	token := getToken(t, h, ad, "" /* , no permissions */)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: test.VersionHeader,
	}.Check(t, h)

	if t.Failed() {
		t.FailNow()
	}
}

func testPullNonExistentTag(t *testing.T, h http.Handler, ad keppel.AuthDriver) {
	token := getToken(t, h, ad, "repository:test1/foo:pull",
		keppel.CanPullFromAccount)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/manifests/latest",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusNotFound,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrManifestUnknown),
	}.Check(t, h)
}

func testPushAndPull(t *testing.T, h http.Handler, ad keppel.AuthDriver, db *keppel.DB, imageConfigJSON, dbContentsBeforeManifestPush, dbContentsAfterManifestPush string) {
	//This tests pushing a minimal image without any layers, so we only upload one object (the config JSON) and create a manifest.
	token := getToken(t, h, ad, "repository:test1/foo:pull,push",
		keppel.CanPullFromAccount,
		keppel.CanPushToAccount)

	//upload config JSON
	bodyBytes, err := ioutil.ReadFile(imageConfigJSON)
	if err != nil {
		t.Fatal(err.Error())
	}
	bodyBytes = bytes.TrimSpace(bodyBytes)
	sha256HashStr := test.UploadBlobToRegistry(t, h, "test1/foo", token, bodyBytes)

	easypg.AssertDBContent(t, db.DbMap.Db, dbContentsBeforeManifestPush)

	//create manifest (this request is executed twice to test idempotency)
	manifestData := assert.JSONObject{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.docker.distribution.manifest.v2+json",
		"config": assert.JSONObject{
			"mediaType": "application/vnd.docker.container.image.v1+json",
			"size":      len(bodyBytes),
			"digest":    "sha256:" + sha256HashStr,
		},
		"layers": []assert.JSONObject{},
	}
	for range []int{1, 2} {
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/v2/test1/foo/manifests/latest",
			Header: map[string]string{
				"Authorization": "Bearer " + token,
				"Content-Type":  "application/vnd.docker.distribution.manifest.v2+json",
			},
			Body:         manifestData,
			ExpectStatus: http.StatusCreated,
			ExpectHeader: test.VersionHeader,
		}.Check(t, h)
		if t.Failed() {
			t.FailNow()
		}
		//check that repo/manifest/tag was created correctly in our DB
		easypg.AssertDBContent(t, db.DbMap.Db, dbContentsAfterManifestPush)
	}

	//verify that "latest" now appears in tag list
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/tags/list",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: test.VersionHeader,
		ExpectBody: assert.JSONObject{
			"name": "test1/foo",
			"tags": []string{"latest"},
		},
	}.Check(t, h)

	//pull manifest using a read-only token
	token = getToken(t, h, ad, "repository:test1/foo:pull",
		keppel.CanPullFromAccount)
	assert.HTTPRequest{
		Method: "GET",
		Path:   "/v2/test1/foo/manifests/latest",
		Header: map[string]string{
			"Accept":        "application/vnd.docker.distribution.manifest.v2+json",
			"Authorization": "Bearer " + token,
		},
		ExpectStatus: http.StatusOK,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Content-Type":        "application/vnd.docker.distribution.manifest.v2+json",
		},
		ExpectBody: manifestData,
	}.Check(t, h)

	//pull config layer
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/blobs/sha256:" + sha256HashStr,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: map[string]string{
			test.VersionHeaderKey: test.VersionHeaderValue,
			"Content-Type":        "application/octet-stream",
		},
		ExpectBody: assert.JSONFixtureFile(imageConfigJSON),
	}.Check(t, h)
}

func testDeleteManifest(t *testing.T, h http.Handler, ad keppel.AuthDriver, db *keppel.DB, digest, dbContentsAfterManifestDelete string) {
	token := getToken(t, h, ad, "repository:test1/foo:delete",
		keppel.CanDeleteFromAccount)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/v2/test1/foo/manifests/" + digest,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusAccepted,
		ExpectHeader: test.VersionHeader,
	}.Check(t, h)
	easypg.AssertDBContent(t, db.DbMap.Db, dbContentsAfterManifestDelete)
}

func testPullExistingNotAllowed(t *testing.T, h http.Handler, ad keppel.AuthDriver) {
	//NOTE: docker-registry sends UNAUTHORIZED (401) instead of DENIED (403)
	//here, but 403 is more correct.

	token := getToken(t, h, ad, "repository:test1/foo:pull" /*, but no perms*/)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/manifests/latest",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusForbidden,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrDenied),
	}.Check(t, h)

	//same if the token is for the wrong scope
	token = getToken(t, h, ad, "repository:test1/bar:pull",
		keppel.CanPullFromAccount)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/manifests/latest",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusForbidden,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrDenied),
	}.Check(t, h)
}

func testKillAndRestartRegistry(t *testing.T, h http.Handler, ad keppel.AuthDriver, od keppel.OrchestrationDriver) {
	//since we already ran some testcases, the keppel-registry for account "test1" should be running
	oe := od.(*orchestration.Engine)
	assert.DeepEqual(t, "OrchestrationEngine state",
		oe.ReportState(),
		map[string]string{"test1": "localhost:10001"},
	)

	//I have a very specific set of skills, skills I have acquired over a very
	//long career. I will look for your keppel-registry process, I will find it,
	//and I *will* kill it.
	cmd := exec.Command("pidof", "keppel-registry")
	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err.Error())
	}
	pid, err := strconv.ParseUint(strings.TrimSpace(string(outputBytes)), 10, 16)
	if err != nil {
		t.Logf("output from `pidof keppel-registry`: %q", string(outputBytes))
		t.Fatal(err.Error())
	}
	proc, err := os.FindProcess(int(pid))
	if err != nil {
		t.Fatal(err.Error())
	}
	err = proc.Kill()
	if err != nil {
		t.Fatal(err.Error())
	}

	//check that the orchestration engine notices what's going on
	time.Sleep(10 * time.Millisecond)
	assert.DeepEqual(t, "OrchestrationEngine state",
		oe.ReportState(),
		map[string]string{}, //empty!
	)

	//check that the next request restarts the keppel-registry instance
	token := getToken(t, h, ad, "repository:test1/doesnotexist:pull",
		keppel.CanPullFromAccount)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/doesnotexist/manifests/latest",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusNotFound,
		ExpectHeader: test.VersionHeader,
		ExpectBody:   test.ErrorCode(keppel.ErrManifestUnknown),
	}.Check(t, h)
	assert.DeepEqual(t, "OrchestrationEngine state",
		oe.ReportState(),
		map[string]string{"test1": "localhost:10002"}, //localprocesses.Driver chooses a new port!
	)
}
