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
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/keppel/internal/api"
	authapi "github.com/sapcc/keppel/internal/api/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func setup(t *testing.T) (http.Handler, keppel.Configuration, *keppel.DB, *test.AuthDriver, *test.StorageDriver, *test.Clock) {
	cfg, db := test.Setup(t)

	//set up a dummy account for testing
	err := db.Insert(&keppel.Account{
		Name:         "test1",
		AuthTenantID: "test1authtenant",
	})
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
	ad, err := keppel.NewAuthDriver("unittest")
	if err != nil {
		t.Fatal(err.Error())
	}
	sd, err := keppel.NewStorageDriver("in-memory-for-testing", ad, cfg)
	if err != nil {
		t.Fatal(err.Error())
	}

	//wire up the HTTP APIs
	clock := &test.Clock{}
	sidGen := &test.StorageIDGenerator{}
	h := api.Compose(
		NewAPI(cfg, sd, db).OverrideTimeNow(clock.Now).OverrideGenerateStorageID(sidGen.Next),
		authapi.NewAPI(cfg, ad, db),
	)

	return h, cfg, db, ad.(*test.AuthDriver), sd.(*test.StorageDriver), clock
}

func getToken(t *testing.T, h http.Handler, ad keppel.AuthDriver, scope string, perms ...keppel.Permission) string {
	t.Helper()

	return ad.(*test.AuthDriver).GetTokenForTest(t, h, "registry.example.org", scope, "test1authtenant", perms...)
}

func getTokenForSecondary(t *testing.T, h http.Handler, ad keppel.AuthDriver, scope string, perms ...keppel.Permission) string {
	t.Helper()

	return ad.(*test.AuthDriver).GetTokenForTest(t, h, "registry-secondary.example.org", scope, "test1authtenant", perms...)
}

//httpTransportForTest is an http.Transport that redirects some
type httpTransportForTest struct {
	Handlers map[string]http.Handler
}

//RoundTrip implements the http.RoundTripper interface.
func (t *httpTransportForTest) RoundTrip(req *http.Request) (*http.Response, error) {
	//only intercept requests when the target host is known to us
	h := t.Handlers[req.URL.Host]
	if h == nil {
		return http.DefaultTransport.RoundTrip(req)
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Result(), nil
}

func sha256Of(data []byte) string {
	sha256Hash := sha256.Sum256(data)
	return hex.EncodeToString(sha256Hash[:])
}

////////////////////////////////////////////////////////////////////////////////
// helpers for setting up test scenarios

func uploadBlob(t *testing.T, h http.Handler, token, fullRepoName string, blob test.Bytes) {
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

func uploadManifest(t *testing.T, h http.Handler, token, fullRepoName string, manifest test.Bytes) {
	assert.HTTPRequest{
		Method: "PUT",
		Path:   fmt.Sprintf("/v2/%s/manifests/%s", fullRepoName, manifest.Digest),
		Header: map[string]string{
			"Authorization": "Bearer " + token,
			"Content-Type":  "application/vnd.docker.distribution.manifest.v2+json",
		},
		Body:         assert.ByteData(manifest.Contents),
		ExpectStatus: http.StatusCreated,
	}.Check(t, h)
}

func getBlobUpload(t *testing.T, h http.Handler, token, fullRepoName string) (uploadURL, uploadUUID string) {
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
	u, _ := getBlobUpload(t, h, token, fullRepoName)
	return u
}

////////////////////////////////////////////////////////////////////////////////
// reusable assertions

func expectBlobExists(t *testing.T, h http.Handler, token, fullRepoName string, blob test.Bytes) {
	for _, method := range []string{"GET", "HEAD"} {
		respBody := blob.Contents
		if method == "HEAD" {
			respBody = nil
		}
		assert.HTTPRequest{
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
		}.Check(t, h)
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
