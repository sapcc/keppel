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

	ch := &composedHandler{}
	c := &Composer{ch}
	m := outermostMiddleware{inner: ch}

	for _, a := range apis {
		switch a := a.(type) {
		case pseudoAPI:
			a.configure(&m)
		default:
			a.AddTo(c)
		}
	}

	return http.Handler(m)
}

// composedHandler is the http.Handler holding all API endpoints that were given to a single [Compose] call.
// The only thing not in here are global middlewares registered via [WithGlobalMiddleware], which are wrapped outside this type:
// The type [outermostMiddleware] is initially constructed holding this type as its inner handler,
// and then middlewares wrap that slot.
//
// This type is separate from [Composer], which constitutes its public interface.
type composedHandler struct {
	tryHandlers []TryHandler
	muxRouter   *mux.Router // initialized when Composer.Router() is first used
}

// ServeHTTP implements the [http.Handler] interface.
func (h *composedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// if we have TryHandlers, use them first (they are likely more efficient than gorilla/mux)
	for _, th := range h.tryHandlers {
		if th.TryServeHTTP(w, r) {
			return
		}
	}

	// check gorilla/mux routes if we have any
	if h.muxRouter != nil {
		h.muxRouter.ServeHTTP(w, r)
		return
	}

	// fallback if nothing matches
	http.NotFound(w, r)
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
