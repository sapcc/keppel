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
	"net/http"
	"net/url"
	"testing"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func setup(t *testing.T) (http.Handler, *test.AuthDriver) {
	apiPublicURL, _ := url.Parse("https://registry.example.org")

	ad, err := keppel.NewAuthDriver("unittest")
	if err != nil {
		t.Fatal(err.Error())
	}
	sd, err := keppel.NewStorageDriver("noop", ad)
	if err != nil {
		t.Fatal(err.Error())
	}
	od, err := keppel.NewOrchestrationDriver("noop", sd)
	if err != nil {
		t.Fatal(err.Error())
	}

	test.Setup(t, &keppel.StateStruct{
		Config:              keppel.Configuration{APIPublicURL: *apiPublicURL},
		AuthDriver:          ad,
		StorageDriver:       sd,
		OrchestrationDriver: od,
	})

	r := mux.NewRouter()
	AddTo(r)

	return r, ad.(*test.AuthDriver)
}

func TestAccountsAPI(t *testing.T) {
	r, authDriver := setup(t)

	//no accounts right now
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: 200,
		ExpectBody:   assert.JSONObject{"accounts": []interface{}{}},
	}.Check(t, r)
	assert.DeepEqual(t, "authDriver.AccountsThatWereSetUp",
		authDriver.AccountsThatWereSetUp,
		[]keppel.Account(nil),
	)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/first",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: 404,
		ExpectBody:   assert.StringData("no such account\n"),
	}.Check(t, r)

	//create an account (this request is executed twice to test idempotency)
	for range []int{1, 2} {
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/accounts/first",
			Header: map[string]string{"X-Test-Perms": "change:tenant1"},
			Body: assert.JSONObject{
				"account": assert.JSONObject{
					"auth_tenant_id": "tenant1",
				},
			},
			ExpectStatus: 200,
			ExpectBody: assert.JSONObject{
				"account": assert.JSONObject{
					"name":           "first",
					"auth_tenant_id": "tenant1",
				},
			},
		}.Check(t, r)
		assert.DeepEqual(t, "authDriver.AccountsThatWereSetUp",
			authDriver.AccountsThatWereSetUp,
			[]keppel.Account{{Name: "first", AuthTenantID: "tenant1"}},
		)
	}

	//check that account shows up in GET...
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: 200,
		ExpectBody: assert.JSONObject{
			"accounts": []assert.JSONObject{{
				"name":           "first",
				"auth_tenant_id": "tenant1",
			}},
		},
	}.Check(t, r)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/first",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: 200,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"name":           "first",
				"auth_tenant_id": "tenant1",
			},
		},
	}.Check(t, r)

	//...but only when one has view permission on the correct tenant
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts",
		Header:       map[string]string{"X-Test-Perms": "view:tenant2"},
		ExpectStatus: 200,
		ExpectBody: assert.JSONObject{
			"accounts": []assert.JSONObject{},
		},
	}.Check(t, r)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/first",
		Header:       map[string]string{"X-Test-Perms": "view:tenant2"},
		ExpectStatus: 404,
		ExpectBody:   assert.StringData("no such account\n"),
	}.Check(t, r)
}

func TestGetAccountsErrorCases(t *testing.T) {
	r, _ := setup(t)

	//test invalid authentication
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts",
		ExpectStatus: 401,
		ExpectBody:   assert.StringData("authentication required: missing X-Test-Perms header\n"),
	}.Check(t, r)
}

func TestPutAccountErrorCases(t *testing.T) {
	r, _ := setup(t)

	//preparation: create an account (so that we can check the error that the requested account name is taken)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
			},
		},
		ExpectStatus: 200,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"name":           "first",
				"auth_tenant_id": "tenant1",
			},
		},
	}.Check(t, r)

	//test invalid inputs
	assert.HTTPRequest{
		Method:       "PUT",
		Path:         "/keppel/v1/accounts/second",
		Header:       map[string]string{"X-Test-Perms": "change:tenant1"},
		Body:         assert.StringData(`{"account":???}`),
		ExpectStatus: 400,
	}.Check(t, r)

	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/second",
		Header: map[string]string{"X-Test-Perms": "change:invalid"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "invalid",
			},
		},
		ExpectStatus: 422,
		ExpectBody:   assert.StringData("malformed attribute \"account.auth_tenant_id\" in request body: must not be \"invalid\"\n"),
	}.Check(t, r)

	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/keppel-api",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
			},
		},
		ExpectStatus: 422,
		ExpectBody:   assert.StringData("account names with the prefix \"keppel-\" are reserved for internal use\n"),
	}.Check(t, r)

	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant2"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant2",
			},
		},
		ExpectStatus: 409,
		ExpectBody:   assert.StringData("account name already in use by a different tenant\n"),
	}.Check(t, r)

	//test invalid authentication/authorization
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/second",
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
			},
		},
		ExpectStatus: 401,
		ExpectBody:   assert.StringData("authentication required: missing X-Test-Perms header\n"),
	}.Check(t, r)

	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/second",
		Header: map[string]string{"X-Test-Perms": "view:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
			},
		},
		ExpectStatus: 403,
		ExpectBody:   assert.StringData("Forbidden\n"),
	}.Check(t, r)
}
