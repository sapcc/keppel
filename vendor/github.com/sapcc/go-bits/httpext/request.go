// SPDX-FileCopyrightText: 2019-2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

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

	// strip port, if any
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}
