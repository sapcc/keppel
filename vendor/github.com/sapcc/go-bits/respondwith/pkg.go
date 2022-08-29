/*******************************************************************************
*
* Copyright 2018 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

// Package respondwith contains some helper functions for generating responses
// in HTTP handlers. Its name is like that because it pairs up with the function
// names in this package, e.g. "respondwith.ErrorText" or "respondwith.JSON".
package respondwith

import (
	"encoding/json"
	"net/http"
)

// JSON serializes the given data into an HTTP response body
// The `code` argument specifies the HTTP response code, usually 200.
func JSON(w http.ResponseWriter, code int, data interface{}) {
	bytes, err := json.Marshal(&data)
	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		w.Write(bytes) //nolint:errcheck
	} else {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ErrorText produces an error response with HTTP status code 500 and
// Content-Type text/plain if the given error is non-nil. Otherwise, nothing is
// done and false is returned. Idiomatic usage looks like this:
//
//	value, err := thisMayFail()
//	if respondwith.ErrorText(w, err) {
//		return
//	}
//
//	useValue(value)
func ErrorText(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}

	http.Error(w, err.Error(), http.StatusInternalServerError)
	return true
}
