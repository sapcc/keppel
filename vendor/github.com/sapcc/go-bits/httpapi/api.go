/******************************************************************************
*
*  Copyright 2022 SAP SE
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
	"net/http"

	"github.com/gorilla/mux"
)

//API is the interface that applications can use to plug their own API
//endpoints into the http.Handler constructed by this package's Compose()
//function.
//
//In this package, some special API instances with names like "With..." and
//"Without..." are available that apply to the entire http.Handler returned by
//Compose(), instead of just adding endpoints to it.
type API interface {
	AddTo(r *mux.Router)
}

//HealthCheckAPI is an API with one endpoint, "GET /healthcheck", that
//usually just prints "ok". If the application knows how to perform a more
//elaborate healthcheck, it can provide a check function in the Check field.
//Failing the application-provided check will cause a 500 response with the
//resulting error message.
type HealthCheckAPI struct {
	SkipRequestLog bool
	Check          func() error //optional
}

//AddTo implements the API interface.
func (h HealthCheckAPI) AddTo(r *mux.Router) {
	r.Methods("GET", "HEAD").Path("/healthcheck").HandlerFunc(h.handleRequest)
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

//A value that can appear as an argument of Compose() without actually being an
//API. The AddTo() implementation is empty; Compose() will call the provided
//configure() method instead.
type pseudoAPI struct {
	configure func(*middleware)
}

func (p pseudoAPI) AddTo(r *mux.Router) {
	//no-op, see above
}

//WithoutLogging can be given as an argument to Compose() to disable request
//logging for the entire http.Handler returned by Compose().
//
//This modifier is intended for use during unit tests.
func WithoutLogging() API {
	return pseudoAPI{
		configure: func(m *middleware) {
			m.skipAllLogs = true
		},
	}
}

//WithGlobalMiddleware can be given as an argument to Compose() to add a
//middleware to the entire http.Handler returned by Compose(). This is a
//similar effect to using mux.Router.Use() inside an API's AddTo() method, but
//explicitly declaring a global middleware like this is clearer than hiding it
//in one specific API implementation.
func WithGlobalMiddleware(globalMiddleware func(http.Handler) http.Handler) API {
	return pseudoAPI{
		configure: func(m *middleware) {
			m.inner = globalMiddleware(m.inner)
		},
	}
}
