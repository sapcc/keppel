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

package test

import (
	"net/http"
	"net/http/httptest"
)

//RoundTripper is a http.RoundTripper that redirects some domains to
//http.Handler instances.
type RoundTripper struct {
	Handlers map[string]http.Handler
}

//RoundTrip implements the http.RoundTripper interface.
func (t *RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	//only intercept requests when the target host is known to us
	h := t.Handlers[req.URL.Host]
	if h == nil {
		return http.DefaultTransport.RoundTrip(req)
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Result(), nil
}
