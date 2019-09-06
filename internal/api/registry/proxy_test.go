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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/assert"
	authapi "github.com/sapcc/keppel/internal/api/auth"
	_ "github.com/sapcc/keppel/internal/drivers/local_processes"
	_ "github.com/sapcc/keppel/internal/drivers/testing"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

const (
	versionHeaderKey   = "Docker-Distribution-Api-Version"
	versionHeaderValue = "registry/2.0"
)

var versionHeader = map[string]string{versionHeaderKey: versionHeaderValue}

//It turns out that starting up a registry takes surprisingly long, so this
//test bundles as many testcases as possible in one run to reduce the waiting.
func TestProxyAPI(t *testing.T) {
	cfg, db := test.Setup(t)

	//set up a dummy account for testing
	err := db.Insert(&keppel.Account{
		Name:         "test1",
		AuthTenantID: "test1authtenant",
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
	r := mux.NewRouter()
	NewAPI(cfg, od, db).AddTo(r)
	authapi.NewAPI(cfg, ad, db).AddTo(r)

	testVersionCheckEndpoint(t, r, ad)
	testPullNonExistentTag(t, r, ad)
	testPushAndPull(t, r, ad)
	testPullExistingNotAllowed(t, r, ad)
}

func testVersionCheckEndpoint(t *testing.T, h http.Handler, ad keppel.AuthDriver) {
	//without token, expect auth challenge
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/",
		ExpectStatus: http.StatusUnauthorized,
		ExpectHeader: map[string]string{
			versionHeaderKey:   versionHeaderValue,
			"Www-Authenticate": `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org"`,
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
		ExpectHeader: versionHeader,
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
		ExpectHeader: versionHeader,
		ExpectBody:   errorCode(keppel.ErrManifestUnknown),
	}.Check(t, h)
}

type byteData []byte

func (b byteData) GetRequestBody() (io.Reader, error) {
	return bytes.NewReader([]byte(b)), nil
}

func testPushAndPull(t *testing.T, h http.Handler, ad keppel.AuthDriver) {
	//This tests pushing a minimal image without any layers, so we only upload one object (the config JSON) and create a manifest.
	token := getToken(t, h, ad, "repository:test1/foo:pull,push",
		keppel.CanPullFromAccount,
		keppel.CanPushToAccount)

	//initiate upload for the image config
	resp, _ := assert.HTTPRequest{
		Method:       "POST",
		Path:         "/v2/test1/foo/blobs/uploads/",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusAccepted,
		ExpectHeader: versionHeader,
	}.Check(t, h)
	if t.Failed() {
		t.FailNow()
	}
	uploadPath := resp.Header.Get("Location")

	//send config data
	bodyBytes, err := ioutil.ReadFile("fixtures/example-docker-image-config.json")
	if err != nil {
		t.Fatal(err.Error())
	}
	bodyBytes = bytes.TrimSpace(bodyBytes)
	resp, _ = assert.HTTPRequest{
		Method: "PATCH",
		Path:   uploadPath,
		Header: map[string]string{
			"Authorization":  "Bearer " + token,
			"Content-Length": fmt.Sprintf("%d", len(bodyBytes)),
			"Content-Range":  fmt.Sprintf("bytes=0-%d", len(bodyBytes)),
			"Content-Type":   "application/octet-stream",
		},
		Body:         byteData(bodyBytes),
		ExpectStatus: http.StatusAccepted,
		ExpectHeader: versionHeader,
	}.Check(t, h)
	if t.Failed() {
		t.FailNow()
	}
	uploadPath = resp.Header.Get("Location")

	//finish config upload
	query := url.Values{}
	sha256Hash := sha256.Sum256(bodyBytes)
	sha256HashStr := hex.EncodeToString(sha256Hash[:])
	query.Set("digest", "sha256:"+sha256HashStr)
	resp, _ = assert.HTTPRequest{
		Method: "PUT",
		Path:   appendQuery(uploadPath, query),
		Header: map[string]string{
			"Authorization":  "Bearer " + token,
			"Content-Length": "0",
		},
		ExpectStatus: http.StatusCreated,
		ExpectHeader: versionHeader,
	}.Check(t, h)
	if t.Failed() {
		t.FailNow()
	}
	layerPath := resp.Header.Get("Location")

	//validate config upload
	resp, _ = assert.HTTPRequest{
		Method:       "HEAD",
		Path:         layerPath,
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
		ExpectHeader: versionHeader,
	}.Check(t, h)
	if t.Failed() {
		t.FailNow()
	}
	assert.DeepEqual(t, "layer Content-Length",
		resp.Header.Get("Content-Length"),
		strconv.FormatUint(uint64(len(bodyBytes)), 10),
	)

	//create manifest
	manifestData := assert.JSONObject{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.docker.distribution.manifest.v2+json",
		"config": assert.JSONObject{
			"mediaType": "application/vnd.docker.container.image.v1+json",
			"size":      1122,
			"digest":    "sha256:" + sha256HashStr,
		},
		"layers": []assert.JSONObject{},
	}
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/v2/test1/foo/manifests/latest",
		Header: map[string]string{
			"Authorization": "Bearer " + token,
			"Content-Type":  "application/vnd.docker.distribution.manifest.v2+json",
		},
		Body:         manifestData,
		ExpectStatus: http.StatusCreated,
		ExpectHeader: versionHeader,
	}.Check(t, h)
	if t.Failed() {
		t.FailNow()
	}

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
			versionHeaderKey: versionHeaderValue,
			"Content-Type":   "application/vnd.docker.distribution.manifest.v2+json",
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
			versionHeaderKey: versionHeaderValue,
			"Content-Type":   "application/octet-stream",
		},
		ExpectBody: assert.JSONFixtureFile("fixtures/example-docker-image-config.json"),
	}.Check(t, h)
}

func appendQuery(url string, query url.Values) string {
	if strings.Contains(url, "?") {
		return url + "&" + query.Encode()
	}
	return url + "?" + query.Encode()
}

func testPullExisting(t *testing.T, h http.Handler, ad keppel.AuthDriver) {
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
		ExpectHeader: versionHeader,
		ExpectBody:   errorCode(keppel.ErrDenied),
	}.Check(t, h)

	//same if the token is for the wrong scope
	token = getToken(t, h, ad, "repository:test1/bar:pull",
		keppel.CanPullFromAccount)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/manifests/latest",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusForbidden,
		ExpectHeader: versionHeader,
		ExpectBody:   errorCode(keppel.ErrDenied),
	}.Check(t, h)
}
