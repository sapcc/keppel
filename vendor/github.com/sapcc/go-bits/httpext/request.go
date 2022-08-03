/*******************************************************************************
*
* Copyright 2019-2020 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

// Package httpext provides some convenience functions on top of the "net/http"
// package from the stdlib.
package httpext

import (
	"net"
	"net/http"
)

// GetRequesterIPFor inspects an http.Request and returns the IP address of the
// machine where the request originated (or the empty string if no IP can be
// found in the request).
func GetRequesterIPFor(r *http.Request) string {
	remoteAddr := r.RemoteAddr
	if xForwardedFor := r.Header.Get("X-Forwarded-For"); xForwardedFor != "" {
		remoteAddr = xForwardedFor
	}

	//strip port, if any
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}
