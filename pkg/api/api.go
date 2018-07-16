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

package api

import (
	"database/sql"
	"net/http"

	"github.com/gophercloud/gophercloud"
	"github.com/gorilla/mux"
)

//KeppelV1 implements the /keppel/v1/ API endpoints.
type KeppelV1 struct {
	db         *sql.DB
	identityV3 *gophercloud.ServiceClient
}

//NewKeppelV1 prepares a new instance of the KeppelV1 API handler.
func NewKeppelV1(db *sql.DB, identityV3 *gophercloud.ServiceClient) (*KeppelV1, error) {
	k := &KeppelV1{
		db:         db,
		identityV3: identityV3,
	}

	return k, nil
}

//Router prepares a http.Handler
func (api *KeppelV1) Router() http.Handler {
	r := mux.NewRouter()

	//NOTE: Keppel account names appear in Swift container names, so they may not
	//contain any slashes.
	r.Methods("GET").Path("/keppel/v1/accounts").HandlerFunc(api.getAccounts)
	r.Methods("GET").Path("/keppel/v1/accounts/{account:[^/]+}").HandlerFunc(api.getAccount)
	r.Methods("PUT").Path("/keppel/v1/accounts/{account:[^/]+}").HandlerFunc(api.putAccount)

	return r
}

func (api *KeppelV1) getAccounts(w http.ResponseWriter, r *http.Request) {
	//TODO
	w.Write([]byte("list accounts"))
}

func (api *KeppelV1) getAccount(w http.ResponseWriter, r *http.Request) {
	//TODO
	w.Write([]byte("get account " + mux.Vars(r)["account"]))
}

func (api *KeppelV1) putAccount(w http.ResponseWriter, r *http.Request) {
	//TODO
	w.Write([]byte("put account " + mux.Vars(r)["account"]))
}
