/*******************************************************************************
*
* Copyright 2021 SAP SE
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

package keppelv1_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/sapcc/go-bits/assert"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestAlternativeAuthSchemes(t *testing.T) {
	s := test.NewSetup(t,
		test.WithKeppelAPI,
		test.WithAccount(models.Account{Name: "test1", AuthTenantID: "tenant1"}),
		test.WithRepo(models.Repository{Name: "foo", AccountName: "test1"}),
	)
	h := s.Handler

	// test anonymous auth: fails without RBAC policy, succeeds with RBAC policy
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories/foo/_manifests",
		Header:       test.AddHeadersForCorrectAuthChallenge(nil),
		ExpectStatus: http.StatusForbidden,
		ExpectHeader: map[string]string{
			"Www-Authenticate": `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="repository:test1/foo:pull"`,
		},
		ExpectBody: assert.StringData("no bearer token found in request headers\n"),
	}.Check(t, h)
	mustExec(t, s.DB, `UPDATE accounts SET rbac_policies_json = $2 WHERE name = $1`, "test1",
		test.ToJSON([]keppel.RBACPolicy{{
			RepositoryPattern: "foo",
			Permissions:       []keppel.RBACPermission{keppel.GrantsAnonymousPull},
		}}),
	)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories/foo/_manifests",
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"manifests": []assert.JSONObject{}},
	}.Check(t, h)
	mustExec(t, s.DB, `UPDATE accounts SET rbac_policies_json = $2 WHERE name = $1`, "test1", "")

	// test bearer token auth: obtain a bearer token on the Auth API while
	// authenticating with Keppel API Auth, then use the bearer token on the
	// Keppel API
	_, respBodyBytes := assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/auth?service=registry.example.org&scope=repository:test1/foo:pull",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,pull:tenant1"},
		ExpectStatus: http.StatusOK,
	}.Check(t, h)
	var tokenData struct {
		Token string `json:"token"`
	}
	err := json.Unmarshal(respBodyBytes, &tokenData)
	if err != nil {
		t.Fatal(err.Error())
	}
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories/foo/_manifests",
		Header:       map[string]string{"Authorization": "Bearer " + tokenData.Token},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"manifests": []assert.JSONObject{}},
	}.Check(t, h)
}
