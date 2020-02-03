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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	authapi "github.com/sapcc/keppel/internal/api/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func setup(t *testing.T) (http.Handler, keppel.Configuration, *keppel.DB, *test.AuthDriver, *test.StorageDriver, *test.Clock) {
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
	r := mux.NewRouter()
	NewAPI(cfg, sd, db).OverrideTimeNow(clock.Now).OverrideGenerateStorageID(sidGen.Next).AddTo(r)
	authapi.NewAPI(cfg, ad, db).AddTo(r)

	return r, cfg, db, ad.(*test.AuthDriver), sd.(*test.StorageDriver), clock
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
