/******************************************************************************
*
*  Copyright 2018-2019 SAP SE
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

package keppelv1

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/sapcc/keppel/internal/keppel"
)

//API contains state variables used by the Keppel V1 API implementation.
type API struct {
	authDriver keppel.AuthDriver
	db         *keppel.DB
}

//NewAPI constructs a new API instance.
func NewAPI(ad keppel.AuthDriver, db *keppel.DB) *API {
	return &API{ad, db}
}

//AddTo adds routes for this API to the given router.
func (a *API) AddTo(r *mux.Router) {
	//NOTE: Keppel account names are severely restricted because Postgres
	//database names are derived from them. Those are, most importantly,
	//case-insensitive and restricted to 64 chars.
	r.Methods("GET").Path("/keppel/v1/accounts").HandlerFunc(a.handleGetAccounts)
	r.Methods("GET").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}").HandlerFunc(a.handleGetAccount)
	r.Methods("PUT").Path("/keppel/v1/accounts/{account:[a-z0-9-]{1,48}}").HandlerFunc(a.handlePutAccount)
}

func respondWithAuthError(w http.ResponseWriter, err *keppel.RegistryV2Error) bool {
	if err == nil {
		return false
	}
	err.WriteAsTextTo(w)
	w.Write([]byte("\n"))
	return true
}
