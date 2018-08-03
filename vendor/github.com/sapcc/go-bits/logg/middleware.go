/*******************************************************************************
*
* Copyright 2018 SAP SE
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

package logg

import (
	"bytes"
	"net"
	"net/http"
)

//Middleware is a HTTP middleware that adds logging of requests and error
//responds to HTTP handlers.
type Middleware struct {
	//Responses with one of these status codes will not be logged.
	ExceptStatusCodes []int
}

//Wrap wraps the given handler with this middleware.
func (m Middleware) Wrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//setup interception of response metadata
		writer := responseWriter{original: w}

		//forward request to actual handler
		h.ServeHTTP(&writer, r)

		//write log line (the format is similar to nginx's "combined" log format, but
		//the timestamp is at the front to ensure consistency with the rest of the
		//log)
		if !containsInt(m.ExceptStatusCodes, writer.statusCode) {
			Other(
				"REQUEST", `%s - - "%s %s %s" %03d %d "%s" "%s"`,
				tryStripPort(r.RemoteAddr),
				r.Method, r.URL.String(), r.Proto,
				writer.statusCode, writer.bytesWritten,
				stringOrDefault("-", r.Header.Get("Referer")),
				stringOrDefault("-", r.Header.Get("User-Agent")),
			)
		}
		if writer.errorMessageBuf.Len() > 0 {
			Error(`during "%s %s": %s`,
				r.Method, r.URL.String(), writer.errorMessageBuf.String(),
			)
		}
	})
}

func containsInt(list []int, value int) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

func stringOrDefault(defaultValue, value string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

func tryStripPort(hostPort string) string {
	host, _, err := net.SplitHostPort(hostPort)
	if err == nil {
		return host
	}
	return hostPort
}

//A custom response writer that collects information about the response to
//later render the request log line.
type responseWriter struct {
	original        http.ResponseWriter
	bytesWritten    uint64
	headersWritten  bool
	statusCode      int
	errorMessageBuf bytes.Buffer
}

//Header implements the http.ResponseWriter interface.
func (w *responseWriter) Header() http.Header {
	return w.original.Header()
}

//Write implements the http.ResponseWriter interface.
func (w *responseWriter) Write(buf []byte) (int, error) {
	if !w.headersWritten {
		w.WriteHeader(http.StatusOK)
	}
	if w.statusCode >= 500 && w.statusCode < 599 {
		//record server errors for the log
		w.errorMessageBuf.Write(buf)
	}
	n, err := w.original.Write(buf)
	w.bytesWritten += uint64(n)
	return n, err
}

//WriteHeader implements the http.ResponseWriter interface.
func (w *responseWriter) WriteHeader(status int) {
	if !w.headersWritten {
		w.original.WriteHeader(status)
		w.statusCode = status
		w.headersWritten = true
	}
}
