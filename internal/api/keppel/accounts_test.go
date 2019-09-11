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

package keppelv1

import (
	"net/http"
	"testing"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/hermes/pkg/cadf"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

type mockAuditor struct {
	Events []cadf.Event
}

func (a *mockAuditor) Record(params audittools.EventParameters) {
	a.Events = append(a.Events, audittools.NewEvent(params))
}

func setup(t *testing.T) (http.Handler, *test.AuthDriver, *test.NameClaimDriver, *mockAuditor) {
	cfg, db := test.Setup(t)

	ad, err := keppel.NewAuthDriver("unittest")
	if err != nil {
		t.Fatal(err.Error())
	}
	ncd, err := keppel.NewNameClaimDriver("unittest", ad, cfg)
	if err != nil {
		t.Fatal(err.Error())
	}

	r := mux.NewRouter()
	auditor := &mockAuditor{}
	NewAPI(ad, ncd, db, auditor).AddTo(r)

	return r, ad.(*test.AuthDriver), ncd.(*test.NameClaimDriver), auditor
}

func TestAccountsAPI(t *testing.T) {
	r, authDriver, _, _ := setup(t)

	//no accounts right now
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
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
		ExpectStatus: http.StatusNotFound,
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
			ExpectStatus: http.StatusOK,
			ExpectBody: assert.JSONObject{
				"account": assert.JSONObject{
					"name":           "first",
					"auth_tenant_id": "tenant1",
					"rbac_policies":  []assert.JSONObject{},
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
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"accounts": []assert.JSONObject{{
				"name":           "first",
				"auth_tenant_id": "tenant1",
				"rbac_policies":  []assert.JSONObject{},
			}},
		},
	}.Check(t, r)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/first",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"name":           "first",
				"auth_tenant_id": "tenant1",
				"rbac_policies":  []assert.JSONObject{},
			},
		},
	}.Check(t, r)

	//...but only when one has view permission on the correct tenant
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts",
		Header:       map[string]string{"X-Test-Perms": "view:tenant2"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"accounts": []assert.JSONObject{},
		},
	}.Check(t, r)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/first",
		Header:       map[string]string{"X-Test-Perms": "view:tenant2"},
		ExpectStatus: http.StatusNotFound,
		ExpectBody:   assert.StringData("no such account\n"),
	}.Check(t, r)

	//create an account with RBAC policies (this request is executed twice to test idempotency)
	rbacPoliciesJSON := []assert.JSONObject{
		{
			"match_repository": "library/.*",
			"permissions":      []string{"anonymous_pull"},
		},
		{
			"match_repository": "library/alpine",
			"match_username":   ".*@tenant2",
			"permissions":      []string{"pull", "push"},
		},
	}
	for range []int{1, 2} {
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/accounts/second",
			Header: map[string]string{"X-Test-Perms": "change:tenant1"},
			Body: assert.JSONObject{
				"account": assert.JSONObject{
					"auth_tenant_id": "tenant1",
					"rbac_policies":  rbacPoliciesJSON,
				},
			},
			ExpectStatus: http.StatusOK,
			ExpectBody: assert.JSONObject{
				"account": assert.JSONObject{
					"name":           "second",
					"auth_tenant_id": "tenant1",
					"rbac_policies":  rbacPoliciesJSON,
				},
			},
		}.Check(t, r)
		assert.DeepEqual(t, "authDriver.AccountsThatWereSetUp",
			authDriver.AccountsThatWereSetUp,
			[]keppel.Account{
				{Name: "first", AuthTenantID: "tenant1"},
				{Name: "second", AuthTenantID: "tenant1"},
			},
		)
	}

	//check that this account also shows up in GET
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"accounts": []assert.JSONObject{
				{
					"name":           "first",
					"auth_tenant_id": "tenant1",
					"rbac_policies":  []assert.JSONObject{},
				},
				{
					"name":           "second",
					"auth_tenant_id": "tenant1",
					"rbac_policies":  rbacPoliciesJSON,
				},
			},
		},
	}.Check(t, r)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/second",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"name":           "second",
				"auth_tenant_id": "tenant1",
				"rbac_policies":  rbacPoliciesJSON,
			},
		},
	}.Check(t, r)
}

func TestGetAccountsErrorCases(t *testing.T) {
	r, _, _, _ := setup(t)

	//test invalid authentication
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts",
		ExpectStatus: http.StatusUnauthorized,
		ExpectBody:   assert.StringData("authentication required: missing X-Test-Perms header\n"),
	}.Check(t, r)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/first",
		ExpectStatus: http.StatusUnauthorized,
		ExpectBody:   assert.StringData("authentication required: missing X-Test-Perms header\n"),
	}.Check(t, r)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
			},
		},
		ExpectStatus: http.StatusUnauthorized,
		ExpectBody:   assert.StringData("authentication required: missing X-Test-Perms header\n"),
	}.Check(t, r)
}

func TestPutAccountErrorCases(t *testing.T) {
	r, _, ncd, _ := setup(t)

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
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"name":           "first",
				"auth_tenant_id": "tenant1",
				"rbac_policies":  []assert.JSONObject{},
			},
		},
	}.Check(t, r)

	//test invalid inputs
	assert.HTTPRequest{
		Method:       "PUT",
		Path:         "/keppel/v1/accounts/second",
		Header:       map[string]string{"X-Test-Perms": "change:tenant1"},
		Body:         assert.StringData(`{"account":???}`),
		ExpectStatus: http.StatusBadRequest,
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
		ExpectStatus: http.StatusUnprocessableEntity,
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
		ExpectStatus: http.StatusUnprocessableEntity,
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
		ExpectStatus: http.StatusConflict,
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
		ExpectStatus: http.StatusUnauthorized,
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
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("Forbidden\n"),
	}.Check(t, r)

	//test rejection by name claim driver
	ncd.CheckFails = true
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/second",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
			},
		},
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("cannot assign name \"second\" to auth tenant \"tenant1\"\n"),
	}.Check(t, r)

	ncd.CheckFails = false
	ncd.CommitFails = true
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/second",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
			},
		},
		ExpectStatus: http.StatusInternalServerError, //this is not a client error because if it was, Check() should have failed already
		ExpectBody:   assert.StringData("failed to assign name \"second\" to auth tenant \"tenant1\"\n"),
	}.Check(t, r)

	//test malformed RBAC policies
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"rbac_policies": []assert.JSONObject{{
					"match_repository": "library/.+",
				}},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("RBAC policy must grant at least one permission\n"),
	}.Check(t, r)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"rbac_policies": []assert.JSONObject{{
					"match_repository": "library/.+",
					"permissions":      []string{"pull", "push", "foo"},
				}},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("\"foo\" is not a valid RBAC policy permission\n"),
	}.Check(t, r)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"rbac_policies": []assert.JSONObject{{
					"permissions": []string{"anonymous_pull"},
				}},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("RBAC policy must have at least one \"match_...\" attribute\n"),
	}.Check(t, r)

	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"rbac_policies": []assert.JSONObject{{
					"match_repository": "library/.+",
					"match_username":   "foo",
					"permissions":      []string{"anonymous_pull"},
				}},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("RBAC policy with \"anonymous_pull\" may not have the \"match_username\" attribute\n"),
	}.Check(t, r)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"rbac_policies": []assert.JSONObject{{
					"match_repository": "library/.+",
					"permissions":      []string{"pull"},
				}},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("RBAC policy with \"pull\" must have the \"match_username\" attribute\n"),
	}.Check(t, r)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"rbac_policies": []assert.JSONObject{{
					"match_repository": "library/.+",
					"permissions":      []string{"push"},
				}},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("RBAC policy with \"push\" must also grant \"pull\"\n"),
	}.Check(t, r)

	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"rbac_policies": []assert.JSONObject{{
					"match_repository": "*/library",
					"permissions":      []string{"anonymous_pull"},
				}},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("\"*/library\" is not a valid regex: error parsing regexp: missing argument to repetition operator: `*`\n"),
	}.Check(t, r)

	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"rbac_policies": []assert.JSONObject{{
					"match_repository": "library/.+",
					"match_username":   "[a-z]++@tenant2",
					"permissions":      []string{"pull"},
				}},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("\"[a-z]++@tenant2\" is not a valid regex: error parsing regexp: invalid nested repetition operator: `++`\n"),
	}.Check(t, r)
}
