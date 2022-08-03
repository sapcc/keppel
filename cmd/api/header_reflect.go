/******************************************************************************
*
*  Copyright 2020 SAP SE
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

package apicmd

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
)

// guiRedirecter is an api.API that implements the GET /debug/reflect-headers endpoint.
type headerReflector struct {
	Enabled bool //usually only on dev/QA systems
}

// AddTo implements the api.API interface.
func (hr *headerReflector) AddTo(r *mux.Router) {
	if hr.Enabled {
		r.Methods("GET").Path("/debug/reflect-headers").HandlerFunc(reflectHeaders)
	}
}

func reflectHeaders(w http.ResponseWriter, r *http.Request) {
	//echo all request headers into the response body
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	for key, vals := range r.Header {
		for _, val := range vals {
			fmt.Fprintf(w, "Request %s: %s\n", key, val)
		}
	}
}
