// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

// Package pprofapi provides a httpapi.API wrapper for the net/http/pprof
// package. This is in a separate package and not the main httpapi package
// because importing net/http/pprof tampers with http.DefaultServeMux, so
// importing this package is only safe if the application does not use
// the http.DefaultServeMux instance.
package pprofapi

import (
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"

	"github.com/gorilla/mux"

	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/httpext"
	"github.com/sapcc/go-bits/logg"
)

// API is a httpapi.API wrapping net/http/pprof. Unlike the default facility in
// net/http/pprof, the respective endpoints are only accessible to admin users.
//
// As an extension of the interface provided by net/http/pprof, the additional
// endpoint `GET /debug/pprof/exe` responds with the process's own executable.
// This can be given to `go tool pprof` when processing any of the pprof
// reports obtained through the other endpoints.
type API struct {
	IsAuthorized func(r *http.Request) bool
}

// AddTo implements the httpapi.API interface.
func (a API) AddTo(r *mux.Router) {
	if a.IsAuthorized == nil {
		panic("API.AddTo() called with IsAuthorized == nil!")
	}

	r.Methods("GET").Path("/debug/pprof/{operation}").HandlerFunc(a.handler)
}

func (a API) handler(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/debug/pprof/:operation")
	httpapi.SkipRequestLog(r)
	if !a.IsAuthorized(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	switch mux.Vars(r)["operation"] {
	default:
		pprof.Index(w, r)
	case "cmdline":
		pprof.Cmdline(w, r)
	case "profile":
		pprof.Profile(w, r)
	case "symbol":
		pprof.Symbol(w, r)
	case "trace":
		pprof.Trace(w, r)
	case "exe":
		// Custom addition: To run `go tool pprof`, we need the executable that
		// produced the pprof output. It is possible to exec into the container to
		// copy the binary file out, or to unpack the image, but since we already
		// obtain the pprof file via HTTP, it's more convenient to obtain the binary
		// over the same mechanism.
		dumpOwnExecutable(w)
	}
}

func dumpOwnExecutable(w http.ResponseWriter) {
	path, err := os.Executable()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(buf)
	if err != nil {
		logg.Error("while writing response body during GET /debug/pprof/exe: %s", err.Error())
	}
}

// IsRequestFromLocalhost checks whether the given request originates from
// `127.0.0.1` or `::1`. It satisfies the interface of API.IsAuthorized.
func IsRequestFromLocalhost(r *http.Request) bool {
	ip := httpext.GetRequesterIPFor(r)
	return ip == "127.0.0.1" || ip == "::1"
}
