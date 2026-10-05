// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"net/http"

	"github.com/gorilla/mux"
	"go.xyrillian.de/gg/pathrouter"
)

var (
	// force imports that are used in docstring links
	_ pathrouter.Matcher = nil
)

// API is the interface that applications can use to plug their own API
// endpoints into the [http.Handler] constructed by this package's [Compose]
// function.
//
// In this package, some special API instances with names like "With..." and
// "Without..." are available that apply to the entire http.Handler returned by
// [Compose], instead of just adding endpoints to it.
type API interface {
	AddTo(c *Composer)
}

// Composer is the argument type given to the AddTo() method of [API].
// API implementations can use the methods on this type to register their endpoints.
type Composer struct {
	h *composedHandler
}

// Router returns a [mux.Router] where APIs can register endpoints.
func (c *Composer) Router() *mux.Router {
	if c.h.muxRouter == nil {
		c.h.muxRouter = mux.NewRouter()
	}
	return c.h.muxRouter
}

// AddTryHandler registers an endpoint or set of endpoints represented as a [TryHandler].
// This can be used e.g. to register [pathrouter.Matcher] instances.
func (c *Composer) AddTryHandler(handler TryHandler) {
	c.h.tryHandlers = append(c.h.tryHandlers, handler)
}

// TryHandler is an interface that works like [http.Handler] with the additional ability of rejecting requests that do not match this handler.
// If false is returned from TryServeHTTP(), the caller shall try to proceed with another handler if possible, or explicitly render a 404 response otherwise.
//
// This interface is implemented e.g. by [pathrouter.Matcher].
type TryHandler interface {
	TryServeHTTP(w http.ResponseWriter, r *http.Request) bool
}

// HealthCheckAPI is an API with one endpoint, "GET /healthcheck", that
// usually just prints "ok". If the application knows how to perform a more
// elaborate healthcheck, it can provide a check function in the Check field.
// Failing the application-provided check will cause a 500 response with the
// resulting error message.
type HealthCheckAPI struct {
	SkipRequestLog bool
	Check          func() error // optional
}

// AddTo implements the API interface.
func (h HealthCheckAPI) AddTo(c *Composer) {
	c.Router().Methods("GET", "HEAD").Path("/healthcheck").HandlerFunc(h.handleRequest)
}

func (h HealthCheckAPI) handleRequest(w http.ResponseWriter, r *http.Request) {
	IdentifyEndpoint(r, "/healthcheck")
	if h.SkipRequestLog {
		SkipRequestLog(r)
	}

	if h.Check != nil {
		err := h.Check()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	http.Error(w, "ok", http.StatusOK)
}

// A value that can appear as an argument of Compose() without actually being an
// API. The AddTo() implementation is empty; Compose() will call the provided
// configure() method instead.
type pseudoAPI struct {
	configure func(*outermostMiddleware)
}

// AddTo implements the API interface.
func (p pseudoAPI) AddTo(c *Composer) {
	// no-op, see above
}

// WithoutLogging can be given as an argument to [Compose] to disable request logging for the entire [http.Handler] returned by it.
//
// This modifier is intended for use during unit tests.
func WithoutLogging() API {
	return pseudoAPI{
		configure: func(m *outermostMiddleware) {
			m.skipAllLogs = true
		},
	}
}

// WithGlobalMiddleware can be given as an argument to [Compose] to add a middleware to the entire [http.Handler] returned by it.
// This should be preferred over using c.Router().Use() inside an API's AddTo() method because:
//
//   - Explicitly declaring a global middleware like this is cleaner than hiding it inside a specific API implementation.
//   - Middlewares declared through this method also affect endpoints located in handlers that do not use gorilla/mux routing.
func WithGlobalMiddleware(globalMiddleware func(http.Handler) http.Handler) API {
	return pseudoAPI{
		configure: func(m *outermostMiddleware) {
			m.inner = globalMiddleware(m.inner)
		},
	}
}
