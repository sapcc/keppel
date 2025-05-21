// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package apicmd

import (
	"fmt"
	"maps"
	"net/http"
	"slices"

	"github.com/gorilla/mux"
)

// guiRedirecter is an api.API that implements the GET /debug/reflect-headers endpoint.
type headerReflector struct {
	Enabled bool // usually only on dev/QA systems
}

// AddTo implements the api.API interface.
func (hr *headerReflector) AddTo(r *mux.Router) {
	if hr.Enabled {
		r.Methods("GET").Path("/debug/reflect-headers").HandlerFunc(reflectHeaders)
	}
}

func reflectHeaders(w http.ResponseWriter, r *http.Request) {
	// echo all request headers into the response body
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	for _, headerName := range slices.Sorted(maps.Keys(r.Header)) {
		vals := r.Header[headerName]
		for _, val := range vals {
			fmt.Fprintf(w, "Request %s: %s\n", headerName, val)
		}
	}
}
