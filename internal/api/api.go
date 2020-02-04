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

package api

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/sre"
)

//API is the generic base type of the API structs exposed by this package's
//child packages.
type API interface {
	AddTo(r *mux.Router)
}

//Compose constructs an http.Handler serving all given APIs.
func Compose(apis ...API) http.Handler {
	r := mux.NewRouter()
	for _, a := range apis {
		a.AddTo(r)
	}
	return sre.Instrument(r)
}
