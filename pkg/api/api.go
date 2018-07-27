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
	"errors"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/gopherpolicy"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/pkg/database"
	"github.com/sapcc/keppel/pkg/openstack"
	"github.com/sapcc/keppel/pkg/orchestrator"
)

//KeppelV1 implements the /keppel/v1/ API endpoints.
type KeppelV1 struct {
	db   *database.DB
	su   *openstack.ServiceUser
	tv   gopherpolicy.Validator
	orch *orchestrator.API
}

//NewKeppelV1 prepares a new KeppelV1 instance.
func NewKeppelV1(db *database.DB, su *openstack.ServiceUser, orch *orchestrator.API) (*KeppelV1, error) {
	tv := gopherpolicy.TokenValidator{
		IdentityV3: su.IdentityV3,
	}
	policyPath := os.Getenv("KEPPEL_POLICY_PATH")
	if policyPath == "" {
		return nil, errors.New("missing env variable: KEPPEL_POLICY_PATH")
	}
	err := tv.LoadPolicyFile(policyPath)
	if err != nil {
		return nil, err
	}

	return &KeppelV1{
		db:   db,
		su:   su,
		tv:   &tv,
		orch: orch,
	}, nil
}

//Router prepares a http.Handler
func (api *KeppelV1) Router() http.Handler {
	r := mux.NewRouter()

	//NOTE: Keppel account names are severely restricted because Postgres
	//database names are derived from them. Those are, most importantly,
	//case-insensitive and restricted to 64 chars.
	r.Methods("GET").Path("/keppel/v1/accounts").HandlerFunc(api.handleGetAccounts)
	r.Methods("GET").Path("/keppel/v1/accounts/{account:[a-z0-9-]{0,48}}").HandlerFunc(api.handleGetAccount)
	r.Methods("PUT").Path("/keppel/v1/accounts/{account:[a-z0-9-]{0,48}}").HandlerFunc(api.handlePutAccount)

	return r
}

func (api *KeppelV1) checkToken(r *http.Request) *gopherpolicy.Token {
	token := api.tv.CheckToken(r)
	token.Context.Logger = logg.Debug
	token.Context.Request = mux.Vars(r)
	return token
}
