// SPDX-FileCopyrightText: 2020-2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"net/http"

	"github.com/gorilla/mux"
)

// Compose constructs an http.Handler serving all the provided APIs. The Handler
// contains a few standard middlewares, as described by the package
// documentation.
func Compose(apis ...API) http.Handler {
	autoConfigureMetricsIfNecessary()

	r := mux.NewRouter()
	m := middleware{inner: r}

	// Automatically identify the endpoint for go-bits metrics using EndpointNamer,
	// called here inside the gorilla/mux chain where route context is available.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if EndpointNamer != nil {
				if name, ok := EndpointNamer(r).Unpack(); ok {
					IdentifyEndpoint(r, name)
				}
			}
			next.ServeHTTP(w, r)
		})
	})

	for _, a := range apis {
		switch a := a.(type) {
		case pseudoAPI:
			a.configure(&m)
		default:
			a.AddTo(r)
		}
	}

	h := http.Handler(m)
	return h
}

type oobKey string

const oobFunctionKey oobKey = "gobits-httpapi-oob"

// An out-of-band message that can be sent from the middleware to the request
// through one of the functions below.
type oobMessage struct {
	SkipLog    bool
	EndpointID string
	UserID     string
}

// SkipRequestLog indicates that this request shall not have a
// "REQUEST" log line written for it.
func SkipRequestLog(r *http.Request) {
	fn, ok := r.Context().Value(oobFunctionKey).(func(oobMessage))
	if !ok {
		panic("httpapi.SkipRequestLog called from request handler outside of httpapi.Compose()!")
	}
	fn(oobMessage{
		SkipLog: true,
	})
}

// IdentifyEndpoint must be called by each endpoint handler in an API that is provided to [Compose].
// It identifies the endpoint for the purpose of HTTP request/response metrics.
func IdentifyEndpoint(r *http.Request, endpoint string) {
	fn, ok := r.Context().Value(oobFunctionKey).(func(oobMessage))
	if !ok {
		panic("httpapi.IdentifyEndpoint called from request handler outside of httpapi.Compose()!")
	}
	fn(oobMessage{
		EndpointID: endpoint,
	})
}

// IdentifyUser may be called inside an endpoint handler in an API that is provided by [Compose].
// It identifies the requesting user within the "REQUEST" log line; the value is considered opaque and logged verbatim.
// If this is never called for a certain request, then "-" will be printed in the log line at the respective location.
func IdentifyUser(r *http.Request, user string) {
	fn, ok := r.Context().Value(oobFunctionKey).(func(oobMessage))
	if !ok {
		panic("httpapi.IdentifyUser called from request handler outside of httpapi.Compose()!")
	}
	fn(oobMessage{
		UserID: user,
	})
}
