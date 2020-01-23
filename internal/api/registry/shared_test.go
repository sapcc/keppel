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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

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
