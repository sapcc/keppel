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

package keppelv1api

import (
	"testing"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/keppel/pkg/test"
)

func TestAccountsAPI(t *testing.T) {
	test.Setup(t, `
		api: { public_url: 'https://registry.example.org' }
		auth: { driver: unittest }
		orchestration: { driver: noop }
		storage: { driver: noop }
	`)

	r := mux.NewRouter()
	AddTo(r)

	assert.HTTPRequest{
		Method:           "GET",
		Path:             "/keppel/v1/accounts",
		ExpectStatusCode: 200,
		ExpectJSON:       "fixtures/accounts-empty.json",
	}.Check(t, r)
}
