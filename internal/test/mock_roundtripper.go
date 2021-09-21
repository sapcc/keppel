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

//WithRoundTripper sets up a RoundTripper instance as the default HTTP
//transport for the duration of the given action.
func WithRoundTripper(action func(*RoundTripper)) {
	t := RoundTripper{Handlers: make(map[string]http.Handler)}
	prevTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = &t
	action(&t)
	http.DefaultClient.Transport = prevTransport
}

//WithoutRoundTripper can be used during WithRoundTripper() to temporarily revert back to the
func WithoutRoundTripper(action func()) {
	prevTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = nil
	action()
	http.DefaultClient.Transport = prevTransport
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
	resp := w.Result()

	//in practice, most HTTP handlers for GET/HEAD requests write into the
	//response body regardless of whether the method was GET or HEAD; strip the
	//response body from HEAD responses to align with net/http's actual behavior
	if req.Method == "HEAD" {
		resp.Body = nil
	}

	return resp, nil
}