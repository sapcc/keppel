/******************************************************************************
*
*  Copyright 2018-2022 SAP SE
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

package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sapcc/go-bits/httpext"
	"github.com/sapcc/go-bits/logg"
)

// A http.Handler middleware that adds all the special behavior for this package.
type middleware struct {
	inner       http.Handler
	skipAllLogs bool
}

// ServeHTTP implements the http.Handler interface.
func (m middleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	skipLog := false
	endpointID := "unknown"

	// provide a back-channel for our custom out-of-band messages to the request handler
	// (this is used by SkipRequestLog etc.)
	ctx := context.WithValue(r.Context(), oobFunctionKey, func(msg oobMessage) {
		if msg.SkipLog {
			skipLog = true
		}
		if msg.EndpointID != "" {
			endpointID = msg.EndpointID
		}
	})
	r = r.WithContext(ctx)

	// setup interception of response metadata
	startedAt := time.Now()
	writer := responseWriter{original: w}

	// forward request to actual handler
	m.inner.ServeHTTP(&writer, r)
	duration := time.Since(startedAt)

	// emit metrics
	labels := getLabels(writer.statusCode, endpointID, r)
	metricResponseDuration.With(labels).Observe(time.Since(startedAt).Seconds())
	if writer.firstByteSentAt != nil {
		metricFirstByteDuration.With(labels).Observe(writer.firstByteSentAt.Sub(startedAt).Seconds())
	}
	metricResponseBodySize.With(labels).Observe(float64(writer.bytesWritten))
	if r.ContentLength != -1 {
		metricRequestBodySize.With(labels).Observe(float64(r.ContentLength))
	}

	// write log line (the format is similar to nginx's "combined" log format, but
	// the timestamp is at the front to ensure consistency with the rest of the
	// log)
	if !m.skipAllLogs {
		if !skipLog || writer.statusCode >= 500 {
			logg.Other(
				"REQUEST", `%s - - "%s %s %s" %03d %d "%s" "%s" %.3fs`,
				httpext.GetRequesterIPFor(r),
				r.Method, r.URL.String(), r.Proto,
				writer.statusCode, writer.bytesWritten,
				stringOrDefault("-", r.Header.Get("Referer")),
				stringOrDefault("-", r.Header.Get("User-Agent")),
				duration.Seconds(),
			)
		}
		if writer.errorMessageBuf.Len() > 0 {
			logg.Error(`during "%s %s": %s`,
				r.Method, r.URL.String(), strings.TrimSpace(writer.errorMessageBuf.String()),
			)
		}
	}
}

func getLabels(statusCode int, endpointID string, r *http.Request) prometheus.Labels {
	l := prometheus.Labels{
		"method":   strings.ToUpper(r.Method),
		"endpoint": endpointID,
		"app":      metricsAppName,
	}

	if statusCode == 0 {
		l["status"] = "200"
	} else {
		l["status"] = strconv.Itoa(statusCode)
	}

	return l
}

func stringOrDefault(defaultValue, value string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

// A custom response writer that collects information about the response to
// later render the request log line.
type responseWriter struct {
	original        http.ResponseWriter
	bytesWritten    uint64
	headersWritten  bool
	statusCode      int
	errorMessageBuf bytes.Buffer
	firstByteSentAt *time.Time
}

// Header implements the http.ResponseWriter interface.
func (w *responseWriter) Header() http.Header {
	return w.original.Header()
}

// Write implements the http.ResponseWriter interface.
func (w *responseWriter) Write(buf []byte) (int, error) {
	if !w.headersWritten {
		w.WriteHeader(http.StatusOK)
	}
	if w.firstByteSentAt == nil {
		now := time.Now()
		w.firstByteSentAt = &now
	}
	if w.statusCode >= 500 {
		// record server errors for the log
		w.errorMessageBuf.Write(buf)
	}
	n, err := w.original.Write(buf)
	if n < 0 {
		return 0, errors.New("original writer returned negative bytes, how?")
	}
	w.bytesWritten += uint64(n)
	return n, err
}

// WriteHeader implements the http.ResponseWriter interface.
func (w *responseWriter) WriteHeader(status int) {
	if !w.headersWritten {
		w.original.WriteHeader(status)
		w.statusCode = status
		w.headersWritten = true
	}
}

// Flush implements the http.Flusher interface.
func (w *responseWriter) Flush() {
	if flusher, ok := w.original.(http.Flusher); ok {
		flusher.Flush()
	}
}
