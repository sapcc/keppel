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
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func runTest(t *testing.T, action func(http.Handler, keppel.AuthDriver)) {
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
	sd, err := keppel.NewStorageDriver("unittest", ad, cfg)
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

	//run the API test
	r := mux.NewRouter()
	NewAPI(cfg, od, db).AddTo(r)
	authapi.NewAPI(cfg, ad, db).AddTo(r)
	action(r, ad)
}

var authorizationHeader = "Basic " + base64.StdEncoding.EncodeToString(
	[]byte("correctusername:correctpassword"),
)

func getToken(t *testing.T, h http.Handler, adGeneric keppel.AuthDriver, scope string, perms ...keppel.Permission) string {
	t.Helper()

	//configure AuthDriver to allow access for this call
	ad := adGeneric.(*test.AuthDriver)
	ad.ExpectedUserName = "correctusername"
	ad.ExpectedPassword = "correctpassword"
	permStrs := make([]string, len(perms))
	for idx, perm := range perms {
		permStrs[idx] = string(perm) + ":test1authtenant"
	}
	ad.GrantedPermissions = strings.Join(permStrs, ",")

	//build a token request
	query := url.Values{}
	query.Set("service", "registry.example.org")
	if scope != "" {
		query.Set("scope", scope)
	}
	_, bodyBytes := assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/auth?" + query.Encode(),
		Header:       map[string]string{"Authorization": authorizationHeader},
		ExpectStatus: http.StatusOK,
	}.Check(t, h)

	var data struct {
		Token string `json:"token"`
	}
	err := json.Unmarshal(bodyBytes, &data)
	if err != nil {
		t.Fatal(err.Error())
	}
	return data.Token
}

func TestAll(t *testing.T) {
	//It turns out that starting up a registry takes surprisingly long, so bundle
	//as many tests as possible in this testcase to reduce the waiting.
	runTest(t, func(h http.Handler, ad keppel.AuthDriver) {

		testVersionCheckEndpoint(t, h, ad)
		testPullNonExistentTag(t, h, ad)
		testPush(t, h, ad)
		//TODO test successful pull
		testPullExistingNotAllowed(t, h, ad)

	})
}

func testVersionCheckEndpoint(t *testing.T, h http.Handler, ad keppel.AuthDriver) {
	//without token, expect auth challenge
	resp, _ := assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/",
		ExpectStatus: http.StatusUnauthorized,
		ExpectBody: assert.JSONObject{
			"errors": []assert.JSONObject{{
				"code":    keppel.ErrUnauthorized,
				"message": "authentication required",
				"detail":  "no bearer token found in request headers",
			}},
		},
	}.Check(t, h)
	assert.DeepEqual(t, "Www-Authenticate header",
		resp.Header.Get("Www-Authenticate"),
		`Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org"`,
	)
	assert.DeepEqual(t, "Docker-Distribution-Api-Version header",
		resp.Header.Get("Docker-Distribution-Api-Version"),
		"registry/2.0",
	)

	//with token, expect status code 200
	token := getToken(t, h, ad, "" /* , no permissions */)
	resp, _ = assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusOK,
	}.Check(t, h)
	assert.DeepEqual(t, "Docker-Distribution-Api-Version header",
		resp.Header.Get("Docker-Distribution-Api-Version"),
		"registry/2.0",
	)

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
		ExpectBody:   errorCode(keppel.ErrManifestUnknown),
	}.Check(t, h)
}

type byteData []byte

func (b byteData) GetRequestBody() (io.Reader, error) {
	return bytes.NewReader([]byte(b)), nil
}

func testPush(t *testing.T, h http.Handler, ad keppel.AuthDriver) {
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
	}.Check(t, h)
	if t.Failed() {
		t.FailNow()
	}
	uploadPath = resp.Header.Get("Location")

	//finish config upload
	query := url.Values{}
	sha256Hash := sha256.Sum256(bodyBytes)
	query.Set("digest", "sha256:"+hex.EncodeToString(sha256Hash[:]))
	resp, _ = assert.HTTPRequest{
		Method: "PUT",
		Path:   appendQuery(uploadPath, query),
		Header: map[string]string{
			"Authorization":  "Bearer " + token,
			"Content-Length": "0",
		},
		ExpectStatus: http.StatusCreated,
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
			"digest":    "sha256:c1b9ce84bd047f52b9e31378d5f2ec9dd4dcca93f6be9395e12f6f658a93d846",
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
	}.Check(t, h)
	if t.Failed() {
		t.FailNow()
	}
}

func appendQuery(url string, query url.Values) string {
	if strings.Contains(url, "?") {
		return url + "&" + query.Encode()
	}
	return url + "?" + query.Encode()
}

func testPullExistingNotAllowed(t *testing.T, h http.Handler, ad keppel.AuthDriver) {
	token := getToken(t, h, ad, "repository:test1/foo:pull" /*, but no perms*/)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/manifests/latest",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusUnauthorized,
		ExpectBody:   errorCode(keppel.ErrUnauthorized),
	}.Check(t, h)

	//same if the token is for the wrong scope
	token = getToken(t, h, ad, "repository:test1/bar:pull",
		keppel.CanPullFromAccount)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/v2/test1/foo/manifests/latest",
		Header:       map[string]string{"Authorization": "Bearer " + token},
		ExpectStatus: http.StatusUnauthorized,
		ExpectBody:   errorCode(keppel.ErrUnauthorized),
	}.Check(t, h)
}
