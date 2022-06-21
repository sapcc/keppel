/******************************************************************************
*
*  Copyright 2020-2022 SAP SE
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

//Compose constructs an http.Handler serving all the provided APIs. The Handler
//contains a few standard middlewares, as described by the package
//documentation.
func Compose(apis ...API) http.Handler {
	autoConfigureMetricsIfNecessary()

	r := mux.NewRouter()
	m := middleware{inner: r}

	for _, a := range apis {
		switch a := a.(type) {
		case pseudoAPI:
			a.configure(&m)
		default:
			a.AddTo(r)
		}
	}

	//TODO add a /healthcheck "ok" API that does SkipRequestLog()
	h := http.Handler(m)
	return h
}

type oobKey string

const OOB_KEY oobKey = "gobits-httpapi-oob"

//An out-of-band message that can be sent from the middleware to the request
//through one of the functions below.
type oobMessage struct {
	SkipLog    bool
	EndpointID string
}

//SkipRequestLog indicates that this request shall not have a
//"REQUEST" log line written for it.
func SkipRequestLog(r *http.Request) {
	fn, ok := r.Context().Value(OOB_KEY).(func(oobMessage))
	if !ok {
		panic("httpapi.SkipRequestLog called from request handler outside of httpapi.Compose()!")
	}
	fn(oobMessage{
		SkipLog: true,
	})
}

//IdentifyEndpoint must be called by each endpoint handler in an API that is
//provided to Compose(). It identifies the endpoint for the purpose of HTTP
//request/response metrics.
func IdentifyEndpoint(r *http.Request, endpoint string) {
	fn, ok := r.Context().Value(OOB_KEY).(func(oobMessage))
	if !ok {
		panic("httpapi.IdentifyEndpoint called from request handler outside of httpapi.Compose()!")
	}
	fn(oobMessage{
		EndpointID: endpoint,
	})
}
