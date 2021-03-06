/*******************************************************************************
*
* Copyright 2018-2020 SAP SE
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
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/hermes/pkg/cadf"
	"github.com/sapcc/keppel/internal/api"
	"github.com/sapcc/keppel/internal/clair"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func setup(t *testing.T) (http.Handler, *test.AuthDriver, *test.FederationDriver, *test.Auditor, keppel.StorageDriver, *keppel.DB, *test.ClairDouble) {
	cfg, db := test.Setup(t, nil)

	//setup a dummy ClairClient for testing the GET vulnerability report endpoint
	claird := test.NewClairDouble()
	clairURL, err := url.Parse("https://clair.example.org/")
	if err != nil {
		t.Fatal(err.Error())
	}
	cfg.ClairClient = &clair.Client{
		BaseURL:      *clairURL,
		PresharedKey: []byte("doesnotmatter"), //since the ClairDouble does not check the Authorization header
	}

	ad, err := keppel.NewAuthDriver("unittest", nil)
	if err != nil {
		t.Fatal(err.Error())
	}
	fd, err := keppel.NewFederationDriver("unittest", ad, cfg)
	if err != nil {
		t.Fatal(err.Error())
	}
	sd, err := keppel.NewStorageDriver("in-memory-for-testing", ad, cfg)
	if err != nil {
		t.Fatal(err.Error())
	}

	auditor := &test.Auditor{}
	h := api.Compose(NewAPI(cfg, ad, fd, sd, db, auditor))

	//second half of the ClairDouble setup
	tt := &test.RoundTripper{
		Handlers: map[string]http.Handler{
			"registry.example.org": h,
			"clair.example.org":    api.Compose(claird),
		},
	}
	http.DefaultClient.Transport = tt

	return h, ad.(*test.AuthDriver), fd.(*test.FederationDriver), auditor, sd, db, claird
}

func TestAccountsAPI(t *testing.T) {
	r, authDriver, fd, auditor, _, _, _ := setup(t)

	//test the /keppel/v1 endpoint
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1",
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"auth_driver": "unittest"},
	}.Check(t, r)

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
	for _, pass := range []int{1, 2} {
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/accounts/first",
			Header: map[string]string{"X-Test-Perms": "change:tenant1"},
			Body: assert.JSONObject{
				"account": assert.JSONObject{
					"auth_tenant_id": "tenant1",
					"metadata": assert.JSONObject{
						"bar": "barbar",
						"foo": "foofoo",
					},
				},
			},
			ExpectStatus: http.StatusOK,
			ExpectBody: assert.JSONObject{
				"account": assert.JSONObject{
					"name":           "first",
					"auth_tenant_id": "tenant1",
					"in_maintenance": false,
					"metadata": assert.JSONObject{
						"bar": "barbar",
						"foo": "foofoo",
					},
					"rbac_policies": []assert.JSONObject{},
				},
			},
		}.Check(t, r)
		assert.DeepEqual(t, "authDriver.AccountsThatWereSetUp",
			authDriver.AccountsThatWereSetUp,
			[]keppel.Account{{Name: "first", AuthTenantID: "tenant1", MetadataJSON: `{"bar":"barbar","foo":"foofoo"}`}},
		)

		//only the first pass should generate an audit event
		if pass == 1 {
			auditor.ExpectEvents(t, cadf.Event{
				RequestPath: "/keppel/v1/accounts/first",
				Action:      "create",
				Outcome:     "success",
				Reason:      test.CADFReasonOK,
				Target: cadf.Resource{
					TypeURI:   "docker-registry/account",
					ID:        "first",
					ProjectID: "tenant1",
				},
			})
		} else {
			auditor.ExpectEvents(t /*, nothing */)
		}
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
				"in_maintenance": false,
				"metadata": assert.JSONObject{
					"bar": "barbar",
					"foo": "foofoo",
				},
				"rbac_policies": []assert.JSONObject{},
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
				"in_maintenance": false,
				"metadata": assert.JSONObject{
					"bar": "barbar",
					"foo": "foofoo",
				},
				"rbac_policies": []assert.JSONObject{},
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
	for _, pass := range []int{1, 2} {
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
					"in_maintenance": false,
					"metadata":       assert.JSONObject{},
					"rbac_policies":  rbacPoliciesJSON,
				},
			},
		}.Check(t, r)
		assert.DeepEqual(t, "authDriver.AccountsThatWereSetUp",
			authDriver.AccountsThatWereSetUp,
			[]keppel.Account{
				{Name: "first", AuthTenantID: "tenant1", MetadataJSON: `{"bar":"barbar","foo":"foofoo"}`},
				{Name: "second", AuthTenantID: "tenant1"},
			},
		)

		//only the first pass should generate audit events
		if pass == 1 {
			auditor.ExpectEvents(t,
				cadf.Event{
					RequestPath: "/keppel/v1/accounts/second",
					Action:      "create",
					Outcome:     "success",
					Reason:      test.CADFReasonOK,
					Target: cadf.Resource{
						TypeURI:   "docker-registry/account",
						ID:        "second",
						ProjectID: "tenant1",
					},
				},
				cadf.Event{
					RequestPath: "/keppel/v1/accounts/second",
					Action:      "create/rbac-policy",
					Outcome:     "success",
					Reason:      test.CADFReasonOK,
					Target: cadf.Resource{
						TypeURI:   "docker-registry/account",
						ID:        "second",
						ProjectID: "tenant1",
						Attachments: []cadf.Attachment{{
							Name:    "payload",
							TypeURI: "mime:application/json",
							Content: test.ToJSON(rbacPoliciesJSON[0]),
						}},
					},
				},
				cadf.Event{
					RequestPath: "/keppel/v1/accounts/second",
					Action:      "create/rbac-policy",
					Outcome:     "success",
					Reason:      test.CADFReasonOK,
					Target: cadf.Resource{
						TypeURI:   "docker-registry/account",
						ID:        "second",
						ProjectID: "tenant1",
						Attachments: []cadf.Attachment{{
							Name:    "payload",
							TypeURI: "mime:application/json",
							Content: test.ToJSON(rbacPoliciesJSON[1]),
						}},
					},
				},
			)
		} else {
			auditor.ExpectEvents(t /*, nothing */)
		}
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
					"in_maintenance": false,
					"metadata": assert.JSONObject{
						"bar": "barbar",
						"foo": "foofoo",
					},
					"rbac_policies": []assert.JSONObject{},
				},
				{
					"name":           "second",
					"auth_tenant_id": "tenant1",
					"in_maintenance": false,
					"metadata":       assert.JSONObject{},
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
				"in_maintenance": false,
				"metadata":       assert.JSONObject{},
				"rbac_policies":  rbacPoliciesJSON,
			},
		},
	}.Check(t, r)

	//check editing of InMaintenance flag
	for _, inMaintenance := range []bool{true, false} {
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/accounts/second",
			Header: map[string]string{"X-Test-Perms": "change:tenant1"},
			Body: assert.JSONObject{
				"account": assert.JSONObject{
					"auth_tenant_id": "tenant1",
					"in_maintenance": inMaintenance,
					"rbac_policies":  rbacPoliciesJSON,
				},
			},
			ExpectStatus: http.StatusOK,
			ExpectBody: assert.JSONObject{
				"account": assert.JSONObject{
					"name":           "second",
					"auth_tenant_id": "tenant1",
					"in_maintenance": inMaintenance,
					"metadata":       assert.JSONObject{},
					"rbac_policies":  rbacPoliciesJSON,
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
					"in_maintenance": inMaintenance,
					"metadata":       assert.JSONObject{},
					"rbac_policies":  rbacPoliciesJSON,
				},
			},
		}.Check(t, r)
	}

	//check editing of RBAC policies
	newRBACPoliciesJSON := []assert.JSONObject{
		//rbacPoliciesJSON[0] is deleted
		//rbacPoliciesJSON[1] is updated as follows:
		{
			"match_repository": "library/alpine",
			"match_username":   ".*@tenant2",
			"permissions":      []string{"pull"},
		},
		//this one is entirely new:
		{
			"match_repository": "library/alpine",
			"match_username":   ".*@tenant3",
			"permissions":      []string{"pull", "delete"},
		},
	}
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/second",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"rbac_policies":  newRBACPoliciesJSON,
			},
		},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"name":           "second",
				"auth_tenant_id": "tenant1",
				"in_maintenance": false,
				"metadata":       assert.JSONObject{},
				"rbac_policies":  newRBACPoliciesJSON,
			},
		},
	}.Check(t, r)
	auditor.ExpectEvents(t,
		cadf.Event{
			RequestPath: "/keppel/v1/accounts/second",
			Action:      "update/rbac-policy",
			Outcome:     "success",
			Reason:      test.CADFReasonOK,
			Target: cadf.Resource{
				TypeURI:   "docker-registry/account",
				ID:        "second",
				ProjectID: "tenant1",
				Attachments: []cadf.Attachment{{
					Name:    "payload",
					TypeURI: "mime:application/json",
					Content: test.ToJSON(newRBACPoliciesJSON[0]),
				}, {
					Name:    "payload-before",
					TypeURI: "mime:application/json",
					Content: test.ToJSON(rbacPoliciesJSON[1]),
				}},
			},
		},
		cadf.Event{
			RequestPath: "/keppel/v1/accounts/second",
			Action:      "create/rbac-policy",
			Outcome:     "success",
			Reason:      test.CADFReasonOK,
			Target: cadf.Resource{
				TypeURI:   "docker-registry/account",
				ID:        "second",
				ProjectID: "tenant1",
				Attachments: []cadf.Attachment{{
					Name:    "payload",
					TypeURI: "mime:application/json",
					Content: test.ToJSON(newRBACPoliciesJSON[1]),
				}},
			},
		},
		cadf.Event{
			RequestPath: "/keppel/v1/accounts/second",
			Action:      "delete/rbac-policy",
			Outcome:     "success",
			Reason:      test.CADFReasonOK,
			Target: cadf.Resource{
				TypeURI:   "docker-registry/account",
				ID:        "second",
				ProjectID: "tenant1",
				Attachments: []cadf.Attachment{{
					Name:    "payload",
					TypeURI: "mime:application/json",
					Content: test.ToJSON(rbacPoliciesJSON[0]),
				}},
			},
		},
	)

	//test setting up a validation policy
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/second",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"rbac_policies":  newRBACPoliciesJSON,
				"validation": assert.JSONObject{
					"required_labels": []string{"foo", "bar"},
				},
			},
		},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"name":           "second",
				"auth_tenant_id": "tenant1",
				"in_maintenance": false,
				"metadata":       assert.JSONObject{},
				"rbac_policies":  newRBACPoliciesJSON,
				"validation": assert.JSONObject{
					"required_labels": []string{"foo", "bar"},
				},
			},
		},
	}.Check(t, r)

	//setting an empty validation policy should be equivalent to removing it
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/second",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"rbac_policies":  newRBACPoliciesJSON,
				"validation": assert.JSONObject{
					"required_labels": []string{},
				},
			},
		},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"name":           "second",
				"auth_tenant_id": "tenant1",
				"in_maintenance": false,
				"metadata":       assert.JSONObject{},
				"rbac_policies":  newRBACPoliciesJSON,
			},
		},
	}.Check(t, r)

	//test POST /keppel/v1/:accounts/sublease success case (error cases are in
	//TestPutAccountErrorCases and TestGetPutAccountReplicationOnFirstUse)
	fd.NextSubleaseTokenSecretToIssue = "this-is-the-token"
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/keppel/v1/accounts/second/sublease",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,change:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"sublease_token": makeSubleaseToken("second", "registry.example.org", "this-is-the-token")},
	}.Check(t, r)
}

func TestGetAccountsErrorCases(t *testing.T) {
	r, _, _, _, _, _, _ := setup(t)

	//test invalid authentication
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts",
		ExpectStatus: http.StatusUnauthorized,
		ExpectBody:   assert.StringData("missing X-Test-Perms header\n"),
	}.Check(t, r)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/first",
		ExpectStatus: http.StatusUnauthorized,
		ExpectBody:   assert.StringData("missing X-Test-Perms header\n"),
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
		ExpectBody:   assert.StringData("missing X-Test-Perms header\n"),
	}.Check(t, r)
}

func TestPutAccountErrorCases(t *testing.T) {
	r, _, fd, _, _, _, _ := setup(t)

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
				"in_maintenance": false,
				"metadata":       assert.JSONObject{},
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
		ExpectBody:   assert.StringData("account names with the prefix \"keppel\" are reserved for internal use\n"),
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
		ExpectBody:   assert.StringData("missing X-Test-Perms header\n"),
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

	//test rejection by federation driver (we test both user error and server
	//error to validate that they generate the correct respective HTTP status
	//codes)
	fd.ClaimFailsBecauseOfUserError = true
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

	fd.ClaimFailsBecauseOfUserError = false
	fd.ClaimFailsBecauseOfServerError = true
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/second",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
			},
		},
		ExpectStatus: http.StatusInternalServerError,
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
					"permissions":      []string{"delete"},
				}},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("RBAC policy with \"delete\" must have the \"match_username\" attribute\n"),
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

	//test unexpected platform filter
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"platform_filter": []assert.JSONObject{{
					"os":           "linux",
					"architecture": "amd64",
				}},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("platform filter is only allowed on replica accounts\n"),
	}.Check(t, r)

	//test errors for sublease token issuance: missing authentication/authorization
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/keppel/v1/accounts/first/sublease",
		ExpectStatus: http.StatusUnauthorized,
		ExpectBody:   assert.StringData("missing X-Test-Perms header\n"),
	}.Check(t, r)
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/keppel/v1/accounts/first/sublease",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("Forbidden\n"),
	}.Check(t, r)
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/keppel/v1/accounts/unknown/sublease", //account does not exist
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,change:tenant1"},
		ExpectStatus: http.StatusNotFound,
		ExpectBody:   assert.StringData("no such account\n"),
	}.Check(t, r)
}

func TestGetPutAccountReplicationOnFirstUse(t *testing.T) {
	r, _, fd, _, _, db, _ := setup(t)

	//configure a peer
	err := db.Insert(&keppel.Peer{HostName: "peer.example.org"})
	if err != nil {
		t.Fatal(err.Error())
	}

	//test error cases on creation
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"replication":    assert.JSONObject{"strategy": "yes_please"},
			},
		},
		ExpectStatus: http.StatusBadRequest,
		ExpectBody:   assert.StringData("request body is not valid JSON: do not know how to deserialize ReplicationPolicy with strategy \"yes_please\"\n"),
	}.Check(t, r)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"replication": assert.JSONObject{
					"strategy": "on_first_use",
					"upstream": "someone-else.example.org",
				},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("unknown peer registry: \"someone-else.example.org\"\n"),
	}.Check(t, r)

	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"replication": assert.JSONObject{
					"strategy": "on_first_use",
					"upstream": "peer.example.org",
				},
			},
		},
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("wrong sublease token\n"),
	}.Check(t, r)

	fd.ValidSubleaseTokenSecrets["first"] = "valid-token"
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{
			"X-Test-Perms":            "change:tenant1",
			"X-Keppel-Sublease-Token": makeSubleaseToken("first", "peer.example.org", "not-the-valid-token"),
		},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"replication": assert.JSONObject{
					"strategy": "on_first_use",
					"upstream": "peer.example.org",
				},
			},
		},
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("wrong sublease token\n"),
	}.Check(t, r)

	//test PUT success case
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{
			"X-Test-Perms":            "change:tenant1",
			"X-Keppel-Sublease-Token": makeSubleaseToken("first", "peer.example.org", "valid-token"),
		},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"replication": assert.JSONObject{
					"strategy": "on_first_use",
					"upstream": "peer.example.org",
				},
			},
		},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"name":           "first",
				"auth_tenant_id": "tenant1",
				"in_maintenance": false,
				"metadata":       assert.JSONObject{},
				"rbac_policies":  []assert.JSONObject{},
				"replication": assert.JSONObject{
					"strategy": "on_first_use",
					"upstream": "peer.example.org",
				},
			},
		},
	}.Check(t, r)

	//PUT on existing account with replication unspecified is okay, leaves
	//replication settings unchanged
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
				"in_maintenance": false,
				"metadata":       assert.JSONObject{},
				"rbac_policies":  []assert.JSONObject{},
				"replication": assert.JSONObject{
					"strategy": "on_first_use",
					"upstream": "peer.example.org",
				},
			},
		},
	}.Check(t, r)

	//cannot issue sublease token for replica account (only for primary accounts)
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/keppel/v1/accounts/first/sublease",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,change:tenant1"},
		ExpectStatus: http.StatusBadRequest,
		ExpectBody:   assert.StringData("operation not allowed for replica accounts\n"),
	}.Check(t, r)

	//PUT on existing account with different replication settings is not allowed
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/second",
		Header: map[string]string{"X-Test-Perms": "change:tenant2"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant2",
			},
		},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"name":           "second",
				"auth_tenant_id": "tenant2",
				"in_maintenance": false,
				"metadata":       assert.JSONObject{},
				"rbac_policies":  []assert.JSONObject{},
			},
		},
	}.Check(t, r)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/second",
		Header: map[string]string{"X-Test-Perms": "change:tenant2"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant2",
				"replication": assert.JSONObject{
					"strategy": "on_first_use",
					"upstream": "peer.example.org",
				},
			},
		},
		ExpectStatus: http.StatusConflict,
		ExpectBody:   assert.StringData("cannot change replication policy on existing account\n"),
	}.Check(t, r)
}

func TestGetPutAccountReplicationFromExternalOnFirstUse(t *testing.T) {
	r, _, fd, _, _, _, _ := setup(t)

	//test error cases on creation
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"replication": assert.JSONObject{
					"strategy": "from_external_on_first_use",
					"upstream": "registry.example.org",
				},
			},
		},
		ExpectStatus: http.StatusBadRequest,
		ExpectBody:   assert.StringData("request body is not valid JSON: json: cannot unmarshal string into Go struct field .account.replication of type keppelv1.ReplicationExternalPeerSpec\n"),
	}.Check(t, r)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"replication": assert.JSONObject{
					"strategy": "from_external_on_first_use",
					"upstream": assert.JSONObject{
						"not": "what-you-expect",
					},
				},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("missing upstream URL for \"from_external_on_first_use\" replication\n"),
	}.Check(t, r)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"replication": assert.JSONObject{
					"strategy": "from_external_on_first_use",
					"upstream": assert.JSONObject{
						"url":      "registry.example.com",
						"username": "keks",
					},
				},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("need either both username and password or neither for \"from_external_on_first_use\" replication\n"),
	}.Check(t, r)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"replication": assert.JSONObject{
					"strategy": "from_external_on_first_use",
					"upstream": assert.JSONObject{
						"url":      "registry.example.com",
						"password": "keks",
					},
				},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("need either both username and password or neither for \"from_external_on_first_use\" replication\n"),
	}.Check(t, r)

	//test PUT success case
	testPlatformFilter := []assert.JSONObject{
		{
			"os":           "linux",
			"architecture": "amd64",
		},
		{
			"os":           "linux",
			"architecture": "arm64",
			"variant":      "v8",
		},
	}
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"replication": assert.JSONObject{
					"strategy": "from_external_on_first_use",
					"upstream": assert.JSONObject{
						"url": "registry.example.com",
					},
				},
				"platform_filter": testPlatformFilter,
			},
		},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"name":           "first",
				"auth_tenant_id": "tenant1",
				"in_maintenance": false,
				"metadata":       assert.JSONObject{},
				"rbac_policies":  []assert.JSONObject{},
				"replication": assert.JSONObject{
					"strategy": "from_external_on_first_use",
					"upstream": assert.JSONObject{
						"url": "registry.example.com",
					},
				},
				"platform_filter": testPlatformFilter,
			},
		},
	}.Check(t, r)

	//PUT on existing account with replication unspecified is okay, leaves
	//replication settings unchanged
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
				"in_maintenance": false,
				"metadata":       assert.JSONObject{},
				"rbac_policies":  []assert.JSONObject{},
				"replication": assert.JSONObject{
					"strategy": "from_external_on_first_use",
					"upstream": assert.JSONObject{
						"url": "registry.example.com",
					},
				},
				"platform_filter": testPlatformFilter,
			},
		},
	}.Check(t, r)

	//test PUT on existing account to update replication credentials
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"replication": assert.JSONObject{
					"strategy": "from_external_on_first_use",
					"upstream": assert.JSONObject{
						"url":      "registry.example.com",
						"username": "foo",
						"password": "bar",
					},
				},
			},
		},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"name":           "first",
				"auth_tenant_id": "tenant1",
				"in_maintenance": false,
				"metadata":       assert.JSONObject{},
				"rbac_policies":  []assert.JSONObject{},
				"replication": assert.JSONObject{
					"strategy": "from_external_on_first_use",
					"upstream": assert.JSONObject{
						"url":      "registry.example.com",
						"username": "foo",
					},
				},
				"platform_filter": testPlatformFilter,
			},
		},
	}.Check(t, r)

	//test sublease token issuance on account (external replicas count as primary
	//accounts for the purposes of account name subleasing)
	fd.NextSubleaseTokenSecretToIssue = "this-is-the-token"
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/keppel/v1/accounts/first/sublease",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,change:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"sublease_token": makeSubleaseToken("first", "registry.example.org", "this-is-the-token")},
	}.Check(t, r)

	//PUT on existing account with different replication settings is not allowed
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"replication": assert.JSONObject{
					"strategy": "from_external_on_first_use",
					"upstream": assert.JSONObject{
						"url":      "other-registry.example.com",
						"username": "foo",
						"password": "bar",
					},
				},
			},
		},
		ExpectStatus: http.StatusConflict,
		ExpectBody:   assert.StringData("cannot change replication policy on existing account\n"),
	}.Check(t, r)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/second",
		Header: map[string]string{"X-Test-Perms": "change:tenant2"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant2",
			},
		},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"name":           "second",
				"auth_tenant_id": "tenant2",
				"in_maintenance": false,
				"metadata":       assert.JSONObject{},
				"rbac_policies":  []assert.JSONObject{},
			},
		},
	}.Check(t, r)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/second",
		Header: map[string]string{"X-Test-Perms": "change:tenant2"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant2",
				"replication": assert.JSONObject{
					"strategy": "from_external_on_first_use",
					"upstream": assert.JSONObject{
						"url":      "other-registry.example.com",
						"username": "foo",
						"password": "bar",
					},
				},
			},
		},
		ExpectStatus: http.StatusConflict,
		ExpectBody:   assert.StringData("cannot change replication policy on existing account\n"),
	}.Check(t, r)

	//PUT on existing account with different platform filter is not allowed
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"replication": assert.JSONObject{
					"strategy": "from_external_on_first_use",
					"upstream": assert.JSONObject{
						"url":      "registry.example.com",
						"username": "foo",
						"password": "bar",
					},
				},
				"platform_filter": []assert.JSONObject{},
			},
		},
		ExpectStatus: http.StatusConflict,
		ExpectBody:   assert.StringData("cannot change platform filter on existing account\n"),
	}.Check(t, r)
}

func TestDeleteAccount(t *testing.T) {
	r, _, fd, _, sd, db, _ := setup(t)

	//setup test accounts and repositories
	nextBlobSweepAt := time.Unix(200, 0)
	accounts := []*keppel.Account{
		{Name: "test1", AuthTenantID: "tenant1", InMaintenance: true, NextBlobSweepedAt: &nextBlobSweepAt},
		{Name: "test2", AuthTenantID: "tenant2", InMaintenance: true},
		{Name: "test3", AuthTenantID: "tenant3", InMaintenance: true},
	}
	for _, account := range accounts {
		mustInsert(t, db, account)
	}
	repos := []*keppel.Repository{
		{AccountName: "test1", Name: "foo/bar"},
		{AccountName: "test1", Name: "something-else"},
	}
	for _, repo := range repos {
		mustInsert(t, db, repo)
	}

	//upload a test image
	image := test.GenerateImage(
		test.GenerateExampleLayer(1),
		test.GenerateExampleLayer(2),
	)

	sidGen := test.StorageIDGenerator{}
	var blobs []keppel.Blob
	for idx, testBlob := range append(image.Layers, image.Config) {
		storageID := sidGen.Next()
		blob := keppel.Blob{
			AccountName: accounts[0].Name,
			Digest:      testBlob.Digest.String(),
			SizeBytes:   uint64(len(testBlob.Contents)),
			StorageID:   storageID,
			PushedAt:    time.Unix(int64(idx), 0),
			ValidatedAt: time.Unix(int64(idx), 0),
		}
		mustInsert(t, db, &blob)
		blobs = append(blobs, blob)

		err := sd.AppendToBlob(*accounts[0], storageID, 1, &blob.SizeBytes, bytes.NewReader(testBlob.Contents))
		if err != nil {
			t.Fatal(err.Error())
		}
		err = sd.FinalizeBlob(*accounts[0], storageID, 1)
		if err != nil {
			t.Fatal(err.Error())
		}
		err = keppel.MountBlobIntoRepo(db, blob, *repos[0])
		if err != nil {
			t.Fatal(err.Error())
		}
	}

	manifest := keppel.Manifest{
		RepositoryID:        repos[0].ID,
		Digest:              image.Manifest.Digest.String(),
		MediaType:           image.Manifest.MediaType,
		SizeBytes:           uint64(len(image.Manifest.Contents)),
		PushedAt:            time.Unix(100, 0),
		ValidatedAt:         time.Unix(100, 0),
		VulnerabilityStatus: clair.PendingVulnerabilityStatus,
	}
	mustInsert(t, db, &manifest)
	err := sd.WriteManifest(*accounts[0], repos[0].Name, image.Manifest.Digest.String(), image.Manifest.Contents)
	if err != nil {
		t.Fatal(err.Error())
	}
	for _, blob := range blobs {
		_, err := db.Exec(
			`INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES ($1, $2, $3)`,
			repos[0].ID, image.Manifest.Digest.String(), blob.ID,
		)
		if err != nil {
			t.Fatal(err.Error())
		}
	}

	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/delete-account-000.sql")

	//failure case: insufficient permissions (the "delete" permission refers to
	//manifests within the account, not the account itself)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
		ExpectStatus: http.StatusForbidden,
	}.Check(t, r)

	//failure case: account not in maintenance
	_, err = db.Exec(`UPDATE accounts SET in_maintenance = FALSE`)
	if err != nil {
		t.Fatal(err.Error())
	}
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,change:tenant1"},
		ExpectStatus: http.StatusConflict,
		ExpectBody: assert.JSONObject{
			"error": "account must be set in maintenance first",
		},
	}.Check(t, r)
	_, err = db.Exec(`UPDATE accounts SET in_maintenance = TRUE`)
	if err != nil {
		t.Fatal(err.Error())
	}

	//phase 1: DELETE on account should complain about remaining manifests
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,change:tenant1"},
		ExpectStatus: http.StatusConflict,
		ExpectBody: assert.JSONObject{
			"remaining_manifests": assert.JSONObject{
				"count": 1,
				"next": []assert.JSONObject{{
					"repository": repos[0].Name,
					"digest":     image.Manifest.Digest.String(),
				}},
			},
		},
	}.Check(t, r)

	//that didn't touch the DB
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/delete-account-000.sql")

	//as indicated by the response, we need to delete the specified manifest to
	//proceed with the account deletion
	assert.HTTPRequest{
		Method: "DELETE",
		Path: fmt.Sprintf(
			"/keppel/v1/accounts/test1/repositories/%s/_manifests/%s",
			repos[0].Name, image.Manifest.Digest.String(),
		),
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
		ExpectStatus: http.StatusNoContent,
	}.Check(t, r)
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/delete-account-001.sql")

	//phase 2: DELETE on account should complain about remaining blobs
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,change:tenant1"},
		ExpectStatus: http.StatusConflict,
		ExpectBody: assert.JSONObject{
			"remaining_blobs": assert.JSONObject{"count": 3},
		},
	}.Check(t, r)

	//but this will have cleaned up the blob mounts and scheduled a GC pass
	//(replace time.Now() with a deterministic time before diffing the DB)
	_, err = db.Exec(
		`UPDATE accounts SET next_blob_sweep_at = $1 WHERE next_blob_sweep_at > $2 AND next_blob_sweep_at <= $3`,
		time.Unix(300, 0),
		time.Now().Add(-5*time.Second),
		time.Now(),
	)
	if err != nil {
		t.Fatal(err.Error())
	}
	//also all blobs will be marked for deletion
	_, err = db.Exec(
		`UPDATE blobs SET can_be_deleted_at = $1 WHERE can_be_deleted_at > $2 AND can_be_deleted_at <= $3`,
		time.Unix(300, 0),
		time.Now().Add(-5*time.Second),
		time.Now(),
	)
	if err != nil {
		t.Fatal(err.Error())
	}
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/delete-account-002.sql")

	//phase 3: all blobs have been cleaned up, so the account can finally be
	//deleted (we use fresh accounts for this because that's easier than
	//running the blob sweep)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test2",
		Header:       map[string]string{"X-Test-Perms": "view:tenant2,change:tenant2"},
		ExpectStatus: http.StatusNoContent,
	}.Check(t, r)

	fd.ForfeitFails = true
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test3",
		Header:       map[string]string{"X-Test-Perms": "view:tenant3,change:tenant3"},
		ExpectStatus: http.StatusConflict,
		ExpectBody: assert.JSONObject{
			"error": "ForfeitAccountName failing as requested",
		},
	}.Check(t, r)

	//account "test2" should be gone now
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/delete-account-003.sql")
}

func makeSubleaseToken(accountName, primaryHostname, secret string) string {
	buf, _ := json.Marshal(assert.JSONObject{
		"account": accountName,
		"primary": primaryHostname,
		"secret":  secret,
	})
	return base64.StdEncoding.EncodeToString(buf)
}
