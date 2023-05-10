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

package keppelv1_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"

	"github.com/sapcc/keppel/internal/clair"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestAccountsAPI(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler
	tr, tr0 := easypg.NewTracker(t, s.DB.DbMap.Db)
	tr0.AssertEmpty()

	//test the /keppel/v1 endpoint
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1",
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"auth_driver": "unittest"},
	}.Check(t, h)

	//no accounts right now
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"accounts": []interface{}{}},
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/first",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("no permission for keppel_account:first:view\n"),
	}.Check(t, h)

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
		}.Check(t, h)

		//only the first pass should generate an audit event
		if pass == 1 {
			s.Auditor.ExpectEvents(t, cadf.Event{
				RequestPath: "/keppel/v1/accounts/first",
				Action:      cadf.CreateAction,
				Outcome:     "success",
				Reason:      test.CADFReasonOK,
				Target: cadf.Resource{
					TypeURI:   "docker-registry/account",
					ID:        "first",
					ProjectID: "tenant1",
				},
			})
		} else {
			s.Auditor.ExpectEvents(t /*, nothing */)
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
	}.Check(t, h)
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
	}.Check(t, h)

	//...but only when one has view permission on the correct tenant
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts",
		Header:       map[string]string{"X-Test-Perms": "view:tenant2"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"accounts": []assert.JSONObject{},
		},
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/first",
		Header:       map[string]string{"X-Test-Perms": "view:tenant2"},
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("no permission for keppel_account:first:view\n"),
	}.Check(t, h)

	//create an account with RBAC policies and GC policies (this request is executed twice to test idempotency)
	gcPoliciesJSON := []assert.JSONObject{
		{
			"match_repository":  ".*/database",
			"except_repository": "archive/.*",
			"time_constraint": assert.JSONObject{
				"on": "pushed_at",
				"newer_than": assert.JSONObject{
					"value": 10,
					"unit":  "d",
				},
			},
			"action": "protect",
		},
		{
			"match_repository": ".*",
			"only_untagged":    true,
			"action":           "delete",
		},
	}
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
					"gc_policies":    gcPoliciesJSON,
					"rbac_policies":  rbacPoliciesJSON,
				},
			},
			ExpectStatus: http.StatusOK,
			ExpectBody: assert.JSONObject{
				"account": assert.JSONObject{
					"name":           "second",
					"auth_tenant_id": "tenant1",
					"gc_policies":    gcPoliciesJSON,
					"in_maintenance": false,
					"metadata":       assert.JSONObject{},
					"rbac_policies":  rbacPoliciesJSON,
				},
			},
		}.Check(t, h)

		//only the first pass should generate audit events
		if pass == 1 {
			s.Auditor.ExpectEvents(t,
				cadf.Event{
					RequestPath: "/keppel/v1/accounts/second",
					Action:      cadf.CreateAction,
					Outcome:     "success",
					Reason:      test.CADFReasonOK,
					Target: cadf.Resource{
						TypeURI:   "docker-registry/account",
						ID:        "second",
						ProjectID: "tenant1",
						Attachments: []cadf.Attachment{{
							Name:    "gc-policies",
							TypeURI: "mime:application/json",
							Content: gcPoliciesToJSON(gcPoliciesJSON),
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
			s.Auditor.ExpectEvents(t /*, nothing */)
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
					"gc_policies":    gcPoliciesJSON,
					"in_maintenance": false,
					"metadata":       assert.JSONObject{},
					"rbac_policies":  rbacPoliciesJSON,
				},
			},
		},
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/second",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"name":           "second",
				"auth_tenant_id": "tenant1",
				"gc_policies":    gcPoliciesJSON,
				"in_maintenance": false,
				"metadata":       assert.JSONObject{},
				"rbac_policies":  rbacPoliciesJSON,
			},
		},
	}.Check(t, h)
	tr.DBChanges().AssertEqual(`
		INSERT INTO accounts (name, auth_tenant_id, metadata_json) VALUES ('first', 'tenant1', '{"bar":"barbar","foo":"foofoo"}');
		INSERT INTO accounts (name, auth_tenant_id, gc_policies_json) VALUES ('second', 'tenant1', '[{"match_repository":".*/database","except_repository":"archive/.*","time_constraint":{"on":"pushed_at","newer_than":{"value":10,"unit":"d"}},"action":"protect"},{"match_repository":".*","only_untagged":true,"action":"delete"}]');
		INSERT INTO rbac_policies (account_name, match_repository, match_username, can_anon_pull) VALUES ('second', 'library/.*', '', TRUE);
		INSERT INTO rbac_policies (account_name, match_repository, match_username, can_pull, can_push) VALUES ('second', 'library/alpine', '.*@tenant2', TRUE, TRUE);
	`)

	//check editing of InMaintenance flag (this also tests editing of GC policies
	//since we don't give any and thus clear the field)
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
		}.Check(t, h)

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
		}.Check(t, h)

		//the first pass also generates an audit event since we're touching the GCPolicies
		if inMaintenance {
			s.Auditor.ExpectEvents(t,
				cadf.Event{
					RequestPath: "/keppel/v1/accounts/second",
					Action:      cadf.UpdateAction,
					Outcome:     "success",
					Reason:      test.CADFReasonOK,
					Target: cadf.Resource{
						TypeURI:   "docker-registry/account",
						ID:        "second",
						ProjectID: "tenant1",
					},
				},
			)
		} else {
			s.Auditor.ExpectEvents(t /*, nothing */)
		}
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
	}.Check(t, h)
	s.Auditor.ExpectEvents(t,
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
	}.Check(t, h)

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
	}.Check(t, h)

	//test POST /keppel/v1/:accounts/sublease success case (error cases are in
	//TestPutAccountErrorCases and TestGetPutAccountReplicationOnFirstUse)
	s.FD.NextSubleaseTokenSecretToIssue = "this-is-the-token"
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/keppel/v1/accounts/second/sublease",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,change:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"sublease_token": makeSubleaseToken("second", "registry.example.org", "this-is-the-token")},
	}.Check(t, h)
	tr.DBChanges().AssertEqual(`
		UPDATE accounts SET gc_policies_json = '[]' WHERE name = 'second';
		DELETE FROM rbac_policies WHERE account_name = 'second' AND match_repository = 'library/.*' AND match_username = '' AND match_cidr = '0.0.0.0/0';
		UPDATE rbac_policies SET can_push = FALSE WHERE account_name = 'second' AND match_repository = 'library/alpine' AND match_username = '.*@tenant2' AND match_cidr = '0.0.0.0/0';
		INSERT INTO rbac_policies (account_name, match_repository, match_username, can_pull, can_delete) VALUES ('second', 'library/alpine', '.*@tenant3', TRUE, TRUE);
	`)
}

func TestGetAccountsErrorCases(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler

	//test invalid authentication (response includes auth challenges since the
	//default auth scheme is bearer token auth)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts",
		ExpectStatus: http.StatusUnauthorized,
		ExpectBody:   assert.StringData("unauthorized\n"),
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/first",
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("no bearer token found in request headers\n"),
		ExpectHeader: map[string]string{
			"Www-Authenticate": `Bearer realm="http://example.com/keppel/v1/auth",service="registry.example.org",scope="keppel_account:first:view"`,
		},
	}.Check(t, h)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
			},
		},
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("no bearer token found in request headers\n"),
		ExpectHeader: map[string]string{
			"Www-Authenticate": `Bearer realm="http://example.com/keppel/v1/auth",service="registry.example.org",scope="keppel_auth_tenant:tenant1:change"`,
		},
	}.Check(t, h)
}

func TestPutAccountErrorCases(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler
	tr, tr0 := easypg.NewTracker(t, s.DB.DbMap.Db)
	tr0.AssertEmpty()

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
	}.Check(t, h)

	//test invalid inputs
	assert.HTTPRequest{
		Method:       "PUT",
		Path:         "/keppel/v1/accounts/second",
		Header:       map[string]string{"X-Test-Perms": "change:tenant1"},
		Body:         assert.StringData(`{"account":???}`),
		ExpectStatus: http.StatusBadRequest,
	}.Check(t, h)

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
	}.Check(t, h)

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
	}.Check(t, h)

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
	}.Check(t, h)

	//test invalid authentication/authorization
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/second",
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
			},
		},
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("no bearer token found in request headers\n"),
		ExpectHeader: map[string]string{
			//default auth is bearer token auth, so an auth challenge gets rendered
			"Www-Authenticate": `Bearer realm="http://example.com/keppel/v1/auth",service="registry.example.org",scope="keppel_auth_tenant:tenant1:change"`,
		},
	}.Check(t, h)

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
		ExpectBody:   assert.StringData("no permission for keppel_auth_tenant:tenant1:change\n"),
	}.Check(t, h)

	//test rejection by federation driver (we test both user error and server
	//error to validate that they generate the correct respective HTTP status
	//codes)
	s.FD.ClaimFailsBecauseOfUserError = true
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
	}.Check(t, h)
	s.FD.ClaimFailsBecauseOfUserError = false

	s.FD.ClaimFailsBecauseOfServerError = true
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
	}.Check(t, h)
	s.FD.ClaimFailsBecauseOfServerError = false

	//test rejection by storage driver
	s.SD.ForbidNewAccounts = true
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/second",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
			},
		},
		ExpectStatus: http.StatusConflict,
		ExpectBody:   assert.StringData("cannot set up backing storage for this account: CanSetupAccount failed as requested\n"),
	}.Check(t, h)
	s.SD.ForbidNewAccounts = false

	//test malformed GC policies
	gcPolicyTestcases := []struct {
		GCPolicyJSON assert.JSONObject
		ErrorMessage string
	}{
		{
			GCPolicyJSON: assert.JSONObject{
				"except_repository": "library/.*",
				"only_untagged":     true,
				"action":            "delete",
			},
			ErrorMessage: `GC policy must have the "match_repository" attribute`,
		},
		{
			GCPolicyJSON: assert.JSONObject{
				"match_repository": "*/library",
				"only_untagged":    true,
				"action":           "delete",
			},
			ErrorMessage: "request body is not valid JSON: \"*/library\" is not a valid regexp: error parsing regexp: missing argument to repetition operator: `*`",
		},
		{
			GCPolicyJSON: assert.JSONObject{
				"match_repository":  "library/.*",
				"except_repository": "*/library",
				"only_untagged":     true,
				"action":            "delete",
			},
			ErrorMessage: "request body is not valid JSON: \"*/library\" is not a valid regexp: error parsing regexp: missing argument to repetition operator: `*`",
		},
		{
			GCPolicyJSON: assert.JSONObject{
				"match_repository": "library/.*",
				"only_untagged":    true,
			},
			ErrorMessage: `GC policy must have the "action" attribute`,
		},
		{
			GCPolicyJSON: assert.JSONObject{
				"match_repository": "library/.*",
				"only_untagged":    true,
				"action":           "foo",
			},
			ErrorMessage: `"foo" is not a valid action for a GC policy`,
		},
		{
			GCPolicyJSON: assert.JSONObject{
				"match_repository": "library/.*",
				"match_tag":        "*-foo",
				"action":           "delete",
			},
			ErrorMessage: "request body is not valid JSON: \"*-foo\" is not a valid regexp: error parsing regexp: missing argument to repetition operator: `*`",
		},
		{
			GCPolicyJSON: assert.JSONObject{
				"match_repository": "library/.*",
				"match_tag":        "foo-.*",
				"except_tag":       "*-bar",
				"action":           "delete",
			},
			ErrorMessage: "request body is not valid JSON: \"*-bar\" is not a valid regexp: error parsing regexp: missing argument to repetition operator: `*`",
		},
		{
			GCPolicyJSON: assert.JSONObject{
				"match_repository": "library/.*",
				"match_tag":        "foo-.*",
				"only_untagged":    true,
				"action":           "delete",
			},
			ErrorMessage: `GC policy cannot have the "match_tag" attribute when "only_untagged" is set`,
		},
		{
			GCPolicyJSON: assert.JSONObject{
				"match_repository": "library/.*",
				"except_tag":       "foo-.*",
				"only_untagged":    true,
				"action":           "delete",
			},
			ErrorMessage: `GC policy cannot have the "except_tag" attribute when "only_untagged" is set`,
		},
		{
			GCPolicyJSON: assert.JSONObject{
				"match_repository": "library/.*",
				"only_untagged":    true,
				"time_constraint":  assert.JSONObject{},
				"action":           "delete",
			},
			ErrorMessage: `GC policy time constraint must have the "on" attribute`,
		},
		{
			GCPolicyJSON: assert.JSONObject{
				"match_repository": "library/.*",
				"only_untagged":    true,
				"time_constraint": assert.JSONObject{
					"on":         "frobnicated_at",
					"newer_than": assert.JSONObject{"value": 10, "unit": "d"},
				},
				"action": "delete",
			},
			ErrorMessage: `"frobnicated_at" is not a valid target for a GC policy time constraint`,
		},
		{
			GCPolicyJSON: assert.JSONObject{
				"match_repository": "library/.*",
				"only_untagged":    true,
				"time_constraint": assert.JSONObject{
					"on": "last_pulled_at",
				},
				"action": "delete",
			},
			ErrorMessage: `GC policy time constraint needs to set at least one attribute other than "on"`,
		},
		{
			GCPolicyJSON: assert.JSONObject{
				"match_repository": "library/.*",
				"only_untagged":    true,
				"time_constraint": assert.JSONObject{
					"on":         "pushed_at",
					"oldest":     10,
					"older_than": assert.JSONObject{"value": 5, "unit": "h"},
				},
				"action": "protect",
			},
			ErrorMessage: `GC policy time constraint cannot set all these attributes at once: "oldest", "older_than"`,
		},
		{
			GCPolicyJSON: assert.JSONObject{
				"match_repository": "library/.*",
				"only_untagged":    true,
				"time_constraint": assert.JSONObject{
					"on":     "pushed_at",
					"oldest": 10,
				},
				"action": "delete",
			},
			ErrorMessage: `GC policy with action "delete" cannot set the "time_constraint.oldest" attribute`,
		},
		{
			GCPolicyJSON: assert.JSONObject{
				"match_repository": "library/.*",
				"only_untagged":    true,
				"time_constraint": assert.JSONObject{
					"on":     "pushed_at",
					"newest": 10,
				},
				"action": "delete",
			},
			ErrorMessage: `GC policy with action "delete" cannot set the "time_constraint.newest" attribute`,
		},
	}
	for _, tc := range gcPolicyTestcases {
		expectedStatus := http.StatusUnprocessableEntity
		if strings.Contains(tc.ErrorMessage, "not valid JSON") {
			expectedStatus = http.StatusBadRequest
		}
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/accounts/first",
			Header: map[string]string{"X-Test-Perms": "change:tenant1"},
			Body: assert.JSONObject{
				"account": assert.JSONObject{
					"auth_tenant_id": "tenant1",
					"gc_policies":    []assert.JSONObject{tc.GCPolicyJSON},
				},
			},
			ExpectStatus: expectedStatus,
			ExpectBody:   assert.StringData(tc.ErrorMessage + "\n"),
		}.Check(t, h)
	}

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
	}.Check(t, h)
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
	}.Check(t, h)
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
	}.Check(t, h)

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
		ExpectBody:   assert.StringData("RBAC policy with \"anonymous_pull\" or \"anonymous_first_pull\" may not have the \"match_username\" attribute\n"),
	}.Check(t, h)
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
		ExpectBody:   assert.StringData("RBAC policy with \"pull\" must have the \"match_cidr\" or \"match_username\" attribute\n"),
	}.Check(t, h)
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
	}.Check(t, h)
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
	}.Check(t, h)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"rbac_policies": []assert.JSONObject{{
					"match_cidr": "0.0.0.0/64",
				}},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("\"0.0.0.0/64\" is not a valid cidr\n"),
	}.Check(t, h)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"rbac_policies": []assert.JSONObject{{
					"match_cidr":       "0.0.0.0/0",
					"match_repository": "test*",
					"permissions":      []string{"pull"},
				}},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("0.0.0.0/0 cannot be used as cidr because it matches everything\n"),
	}.Check(t, h)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"rbac_policies": []assert.JSONObject{{
					"match_cidr":  "1.2.3.4/16",
					"permissions": []string{"pull"},
				}},
			},
		},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"in_maintenance": false,
				"metadata":       assert.JSONObject{},
				"name":           "first",
				"rbac_policies": []assert.JSONObject{{
					"match_cidr":  "1.2.0.0/16",
					"permissions": []string{"pull"},
				}},
			},
		},
	}.Check(t, h)
	tr.DBChanges().AssertEqual(`
		INSERT INTO accounts (name, auth_tenant_id) VALUES ('first', 'tenant1');
		INSERT INTO rbac_policies (account_name, match_repository, match_username, can_pull, match_cidr) VALUES ('first', '', '', TRUE, '1.2.0.0/16');
	`)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/first",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"in_maintenance": false,
				"metadata":       assert.JSONObject{},
				"name":           "first",
				"rbac_policies": []assert.JSONObject{{
					"match_cidr":  "1.2.0.0/16",
					"permissions": []string{"pull"},
				}},
			},
		},
	}.Check(t, h)
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
					"permissions":      []string{"anonymous_first_pull"},
				}},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("RBAC policy with \"anonymous_pull\" or \"anonymous_first_pull\" may not have the \"match_username\" attribute\n"),
	}.Check(t, h)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"rbac_policies": []assert.JSONObject{{
					"match_repository": "library/.+",
					"permissions":      []string{"anonymous_first_pull"},
				}},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("RBAC policy with \"anonymous_first_pull\" may only be for external replica accounts\n"),
	}.Check(t, h)

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
		ExpectStatus: http.StatusBadRequest,
		ExpectBody:   assert.StringData("request body is not valid JSON: \"*/library\" is not a valid regexp: error parsing regexp: missing argument to repetition operator: `*`\n"),
	}.Check(t, h)
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
		ExpectStatus: http.StatusBadRequest,
		ExpectBody:   assert.StringData("request body is not valid JSON: \"[a-z]++@tenant2\" is not a valid regexp: error parsing regexp: invalid nested repetition operator: `++`\n"),
	}.Check(t, h)

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
	}.Check(t, h)

	//test errors for sublease token issuance: missing authentication/authorization
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/keppel/v1/accounts/first/sublease",
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("no bearer token found in request headers\n"),
		ExpectHeader: map[string]string{
			//default auth is bearer token auth, so an auth challenge gets rendered
			"Www-Authenticate": `Bearer realm="http://example.com/keppel/v1/auth",service="registry.example.org",scope="keppel_account:first:change"`,
		},
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/keppel/v1/accounts/first/sublease",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("no permission for keppel_account:first:change\n"),
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/keppel/v1/accounts/unknown/sublease", //account does not exist
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,change:tenant1"},
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("no permission for keppel_account:unknown:change\n"),
	}.Check(t, h)
}

func TestGetPutAccountReplicationOnFirstUse(t *testing.T) {
	test.WithRoundTripper(func(tt *test.RoundTripper) {
		s1 := test.NewSetup(t, test.WithKeppelAPI, test.WithPeerAPI)
		s2 := test.NewSetup(t, test.WithKeppelAPI, test.IsSecondaryTo(&s1))

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
		}.Check(t, s1.Handler)

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
		}.Check(t, s2.Handler)
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
		}.Check(t, s2.Handler)

		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/accounts/first",
			Header: map[string]string{"X-Test-Perms": "change:tenant1"},
			Body: assert.JSONObject{
				"account": assert.JSONObject{
					"auth_tenant_id": "tenant1",
					"replication": assert.JSONObject{
						"strategy": "on_first_use",
						"upstream": "registry.example.org",
					},
				},
			},
			ExpectStatus: http.StatusForbidden,
			ExpectBody:   assert.StringData("wrong sublease token\n"),
		}.Check(t, s2.Handler)

		s2.FD.ValidSubleaseTokenSecrets["first"] = "valid-token"
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/accounts/first",
			Header: map[string]string{
				"X-Test-Perms":            "change:tenant1",
				"X-Keppel-Sublease-Token": makeSubleaseToken("first", "registry.example.org", "not-the-valid-token"),
			},
			Body: assert.JSONObject{
				"account": assert.JSONObject{
					"auth_tenant_id": "tenant1",
					"replication": assert.JSONObject{
						"strategy": "on_first_use",
						"upstream": "registry.example.org",
					},
				},
			},
			ExpectStatus: http.StatusForbidden,
			ExpectBody:   assert.StringData("wrong sublease token\n"),
		}.Check(t, s2.Handler)

		//test PUT success case
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/accounts/first",
			Header: map[string]string{
				"X-Test-Perms":            "change:tenant1",
				"X-Keppel-Sublease-Token": makeSubleaseToken("first", "registry.example.org", "valid-token"),
			},
			Body: assert.JSONObject{
				"account": assert.JSONObject{
					"auth_tenant_id": "tenant1",
					"replication": assert.JSONObject{
						"strategy": "on_first_use",
						"upstream": "registry.example.org",
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
						"upstream": "registry.example.org",
					},
				},
			},
		}.Check(t, s2.Handler)

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
						"upstream": "registry.example.org",
					},
				},
			},
		}.Check(t, s2.Handler)

		//cannot issue sublease token for replica account (only for primary accounts)
		assert.HTTPRequest{
			Method:       "POST",
			Path:         "/keppel/v1/accounts/first/sublease",
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,change:tenant1"},
			ExpectStatus: http.StatusBadRequest,
			ExpectBody:   assert.StringData("operation not allowed for replica accounts\n"),
		}.Check(t, s2.Handler)

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
		}.Check(t, s2.Handler)
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/accounts/second",
			Header: map[string]string{"X-Test-Perms": "change:tenant2"},
			Body: assert.JSONObject{
				"account": assert.JSONObject{
					"auth_tenant_id": "tenant2",
					"replication": assert.JSONObject{
						"strategy": "on_first_use",
						"upstream": "registry.example.org",
					},
				},
			},
			ExpectStatus: http.StatusConflict,
			ExpectBody:   assert.StringData("cannot change replication policy on existing account\n"),
		}.Check(t, s2.Handler)
	})
}

func TestGetPutAccountReplicationFromExternalOnFirstUse(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler

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
	}.Check(t, h)
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
	}.Check(t, h)
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
	}.Check(t, h)
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
	}.Check(t, h)

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
	}.Check(t, h)

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
	}.Check(t, h)

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
	}.Check(t, h)

	//PUT on existing account with replication credentials section copied from
	//GET is okay, leaves replication settings unchanged too (this is important
	//because, in practice, clients copy the account config from GET, change a
	//thing, and PUT the result)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
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
	}.Check(t, h)

	//...but changing the username without also supplying a password is wrong
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"in_maintenance": false,
				"metadata":       assert.JSONObject{},
				"rbac_policies":  []assert.JSONObject{},
				"replication": assert.JSONObject{
					"strategy": "from_external_on_first_use",
					"upstream": assert.JSONObject{
						"url":      "registry.example.com",
						"username": "bar",
					},
				},
				"platform_filter": testPlatformFilter,
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("cannot change username for \"from_external_on_first_use\" replication without also changing password\n"),
	}.Check(t, h)

	//test sublease token issuance on account (external replicas count as primary
	//accounts for the purposes of account name subleasing)
	s.FD.NextSubleaseTokenSecretToIssue = "this-is-the-token"
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/keppel/v1/accounts/first/sublease",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,change:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"sublease_token": makeSubleaseToken("first", "registry.example.org", "this-is-the-token")},
	}.Check(t, h)

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
	}.Check(t, h)
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
	}.Check(t, h)
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
	}.Check(t, h)

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
	}.Check(t, h)
}

func uploadManifest(t *testing.T, s test.Setup, account *keppel.Account, repo *keppel.Repository, manifest test.Bytes, sizeBytes uint64) keppel.Manifest {
	t.Helper()

	dbManifest := keppel.Manifest{
		RepositoryID: repo.ID,
		Digest:       manifest.Digest,
		MediaType:    manifest.MediaType,
		SizeBytes:    sizeBytes,
		PushedAt:     s.Clock.Now(),
		ValidatedAt:  s.Clock.Now(),
	}
	mustDo(t, s.DB.Insert(&dbManifest))
	mustDo(t, s.DB.Insert(&keppel.VulnerabilityInfo{
		RepositoryID: repo.ID,
		Digest:       manifest.Digest,
		NextCheckAt:  time.Unix(0, 0),
		Status:       clair.PendingVulnerabilityStatus,
	}))
	mustDo(t, s.DB.Insert(&keppel.ManifestContent{
		RepositoryID: repo.ID,
		Digest:       manifest.Digest.String(),
		Content:      manifest.Contents,
	}))
	mustDo(t, s.SD.WriteManifest(*account, repo.Name, manifest.Digest, manifest.Contents))
	return dbManifest
}

func TestDeleteAccount(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler

	//setup test accounts and repositories
	nextBlobSweepAt := time.Unix(200, 0)
	accounts := []*keppel.Account{
		{Name: "test1", AuthTenantID: "tenant1", InMaintenance: true, NextBlobSweepedAt: &nextBlobSweepAt, GCPoliciesJSON: "[]"},
		{Name: "test2", AuthTenantID: "tenant2", InMaintenance: true, GCPoliciesJSON: "[]"},
		{Name: "test3", AuthTenantID: "tenant3", InMaintenance: true, GCPoliciesJSON: "[]"},
	}
	for _, account := range accounts {
		mustInsert(t, s.DB, account)
	}
	repos := []*keppel.Repository{
		{AccountName: "test1", Name: "foo/bar"},
		{AccountName: "test1", Name: "something-else"},
	}
	for _, repo := range repos {
		mustInsert(t, s.DB, repo)
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
			Digest:      testBlob.Digest,
			SizeBytes:   uint64(len(testBlob.Contents)),
			StorageID:   storageID,
			PushedAt:    time.Unix(int64(idx), 0),
			ValidatedAt: time.Unix(int64(idx), 0),
		}
		mustInsert(t, s.DB, &blob)
		blobs = append(blobs, blob)

		err := s.SD.AppendToBlob(*accounts[0], storageID, 1, &blob.SizeBytes, bytes.NewReader(testBlob.Contents))
		if err != nil {
			t.Fatal(err.Error())
		}
		err = s.SD.FinalizeBlob(*accounts[0], storageID, 1)
		if err != nil {
			t.Fatal(err.Error())
		}
		err = keppel.MountBlobIntoRepo(s.DB, blob, *repos[0])
		if err != nil {
			t.Fatal(err.Error())
		}
	}

	mustInsert(t, s.DB, &keppel.Manifest{
		RepositoryID: repos[0].ID,
		Digest:       image.Manifest.Digest,
		MediaType:    image.Manifest.MediaType,
		SizeBytes:    uint64(len(image.Manifest.Contents)),
		PushedAt:     time.Unix(100, 0),
		ValidatedAt:  time.Unix(100, 0),
	})
	mustInsert(t, s.DB, &keppel.VulnerabilityInfo{
		RepositoryID: repos[0].ID,
		Digest:       image.Manifest.Digest,
		NextCheckAt:  time.Unix(0, 0),
		Status:       clair.PendingVulnerabilityStatus,
	})
	err := s.SD.WriteManifest(*accounts[0], repos[0].Name, image.Manifest.Digest, image.Manifest.Contents)
	if err != nil {
		t.Fatal(err.Error())
	}
	for _, blob := range blobs {
		_, err := s.DB.Exec(
			`INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES ($1, $2, $3)`,
			repos[0].ID, image.Manifest.Digest.String(), blob.ID,
		)
		if err != nil {
			t.Fatal(err.Error())
		}
	}

	imageList := test.GenerateImageList(image)
	uploadManifest(t, s, accounts[0], repos[0], imageList.Manifest, imageList.SizeBytes())
	mustExec(t, s.DB,
		`INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES ($1, $2, $3)`,
		repos[0].ID, imageList.Manifest.Digest.String(), image.Manifest.Digest.String(),
	)

	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/delete-account-000.sql")

	//failure case: insufficient permissions (the "delete" permission refers to
	//manifests within the account, not the account itself)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
		ExpectStatus: http.StatusForbidden,
	}.Check(t, h)

	//failure case: account not in maintenance
	_, err = s.DB.Exec(`UPDATE accounts SET in_maintenance = FALSE`)
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
	}.Check(t, h)
	_, err = s.DB.Exec(`UPDATE accounts SET in_maintenance = TRUE`)
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
				"count": 2,
				"next": []assert.JSONObject{{
					"repository": repos[0].Name,
					"digest":     imageList.Manifest.Digest.String(),
				}},
			},
		},
	}.Check(t, h)

	//that didn't touch the DB
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/delete-account-000.sql")

	//as indicated by the response, we need to delete the specified manifest to
	//proceed with the account deletion
	assert.HTTPRequest{
		Method: "DELETE",
		Path: fmt.Sprintf(
			"/keppel/v1/accounts/test1/repositories/%s/_manifests/%s",
			repos[0].Name, imageList.Manifest.Digest.String(),
		),
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
		ExpectStatus: http.StatusNoContent,
	}.Check(t, h)

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
	}.Check(t, h)

	assert.HTTPRequest{
		Method: "DELETE",
		Path: fmt.Sprintf(
			"/keppel/v1/accounts/test1/repositories/%s/_manifests/%s",
			repos[0].Name, image.Manifest.Digest.String(),
		),
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
		ExpectStatus: http.StatusNoContent,
	}.Check(t, h)
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/delete-account-001.sql")

	//phase 2: DELETE on account should complain about remaining blobs
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,change:tenant1"},
		ExpectStatus: http.StatusConflict,
		ExpectBody: assert.JSONObject{
			"remaining_blobs": assert.JSONObject{"count": 3},
		},
	}.Check(t, h)

	//but this will have cleaned up the blob mounts and scheduled a GC pass
	//(replace time.Now() with a deterministic time before diffing the DB)
	_, err = s.DB.Exec(
		`UPDATE accounts SET next_blob_sweep_at = $1 WHERE next_blob_sweep_at > $2 AND next_blob_sweep_at <= $3`,
		time.Unix(300, 0),
		time.Now().Add(-5*time.Second),
		time.Now(),
	)
	if err != nil {
		t.Fatal(err.Error())
	}
	//also all blobs will be marked for deletion
	_, err = s.DB.Exec(
		`UPDATE blobs SET can_be_deleted_at = $1 WHERE can_be_deleted_at > $2 AND can_be_deleted_at <= $3`,
		time.Unix(300, 0),
		time.Now().Add(-5*time.Second),
		time.Now(),
	)
	if err != nil {
		t.Fatal(err.Error())
	}
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/delete-account-002.sql")

	//phase 3: all blobs have been cleaned up, so the account can finally be
	//deleted (we use fresh accounts for this because that's easier than
	//running the blob sweep)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test2",
		Header:       map[string]string{"X-Test-Perms": "view:tenant2,change:tenant2"},
		ExpectStatus: http.StatusNoContent,
	}.Check(t, h)

	s.FD.ForfeitFails = true
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test3",
		Header:       map[string]string{"X-Test-Perms": "view:tenant3,change:tenant3"},
		ExpectStatus: http.StatusConflict,
		ExpectBody: assert.JSONObject{
			"error": "ForfeitAccountName failing as requested",
		},
	}.Check(t, h)

	//account "test2" should be gone now
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/delete-account-003.sql")
}

//nolint:unparam
func makeSubleaseToken(accountName, primaryHostname, secret string) string {
	buf, _ := json.Marshal(assert.JSONObject{
		"account": accountName,
		"primary": primaryHostname,
		"secret":  secret,
	})
	return base64.StdEncoding.EncodeToString(buf)
}

func gcPoliciesToJSON(in []assert.JSONObject) string {
	//This is mostly the same as `test.ToJSON(in)`, but deserializes into
	//keppel.GCPolicy in an intermediate step to render the JSON with the correct
	//field order.
	buf, _ := json.Marshal(in)
	var policies []keppel.GCPolicy
	err := json.Unmarshal(buf, &policies)
	if err != nil {
		panic(err.Error())
	}
	buf, err = json.Marshal(policies)
	if err != nil {
		panic(err.Error())
	}
	return string(buf)
}

func TestReplicaAccountsInheritPlatformFilter(t *testing.T) {
	test.WithRoundTripper(func(tt *test.RoundTripper) {
		s1 := test.NewSetup(t, test.WithKeppelAPI, test.WithPeerAPI)
		s2 := test.NewSetup(t, test.WithKeppelAPI, test.IsSecondaryTo(&s1))

		testPlatformFilter := []assert.JSONObject{
			{
				"os":           "linux",
				"architecture": "amd64",
			},
		}

		// create some primary accounts to play with
		for _, name := range []string{"first", "second", "third"} {
			assert.HTTPRequest{
				Method: "PUT",
				Path:   fmt.Sprintf("/keppel/v1/accounts/%s", name),
				Header: map[string]string{"X-Test-Perms": "change:tenant1"},
				Body: assert.JSONObject{
					"account": assert.JSONObject{
						"auth_tenant_id": "tenant1",
						"replication": assert.JSONObject{
							"strategy": "from_external_on_first_use",
							"upstream": assert.JSONObject{
								"url": "registry.example.org",
							},
						},
						"platform_filter": testPlatformFilter,
					},
				},
				ExpectStatus: http.StatusOK,
				ExpectBody: assert.JSONObject{
					"account": assert.JSONObject{
						"name":           name,
						"auth_tenant_id": "tenant1",
						"in_maintenance": false,
						"metadata":       assert.JSONObject{},
						"rbac_policies":  []assert.JSONObject{},
						"replication": assert.JSONObject{
							"strategy": "from_external_on_first_use",
							"upstream": assert.JSONObject{
								"url": "registry.example.org",
							},
						},
						"platform_filter": testPlatformFilter,
					},
				},
			}.Check(t, s1.Handler)
			s2.FD.ValidSubleaseTokenSecrets[name] = "valid-token"
		}

		//create an account which inherits the PlatformFilter
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/accounts/first",
			Header: map[string]string{
				"X-Test-Perms":            "change:tenant1",
				"X-Keppel-Sublease-Token": makeSubleaseToken("first", "registry.example.org", "valid-token"),
			},
			Body: assert.JSONObject{
				"account": assert.JSONObject{
					"auth_tenant_id": "tenant1",
					"replication": assert.JSONObject{
						"strategy": "on_first_use",
						"upstream": "registry.example.org",
					},
				},
			},
			ExpectStatus: http.StatusOK,
			ExpectBody: assert.JSONObject{
				"account": assert.JSONObject{
					"name":            "first",
					"auth_tenant_id":  "tenant1",
					"in_maintenance":  false,
					"metadata":        assert.JSONObject{},
					"platform_filter": testPlatformFilter,
					"rbac_policies":   []assert.JSONObject{},
					"replication": assert.JSONObject{
						"strategy": "on_first_use",
						"upstream": "registry.example.org",
					},
				},
			},
		}.Check(t, s2.Handler)

		//create an account with the same PlatformFilter
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/accounts/second",
			Header: map[string]string{
				"X-Test-Perms":            "change:tenant1",
				"X-Keppel-Sublease-Token": makeSubleaseToken("second", "registry.example.org", "valid-token"),
			},
			Body: assert.JSONObject{
				"account": assert.JSONObject{
					"auth_tenant_id": "tenant1",
					"platform_filter": []assert.JSONObject{{
						"os":           "linux",
						"architecture": "amd64",
					}},
					"replication": assert.JSONObject{
						"strategy": "on_first_use",
						"upstream": "registry.example.org",
					},
				},
			},
			ExpectStatus: http.StatusOK,
			ExpectBody: assert.JSONObject{
				"account": assert.JSONObject{
					"name":            "second",
					"auth_tenant_id":  "tenant1",
					"in_maintenance":  false,
					"metadata":        assert.JSONObject{},
					"platform_filter": testPlatformFilter,
					"rbac_policies":   []assert.JSONObject{},
					"replication": assert.JSONObject{
						"strategy": "on_first_use",
						"upstream": "registry.example.org",
					},
				},
			},
		}.Check(t, s2.Handler)

		//create an account with an incompatible PlatformFilter
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/accounts/third",
			Header: map[string]string{
				"X-Test-Perms":            "change:tenant1",
				"X-Keppel-Sublease-Token": makeSubleaseToken("third", "registry.example.org", "valid-token"),
			},
			Body: assert.JSONObject{
				"account": assert.JSONObject{
					"auth_tenant_id": "tenant1",
					"platform_filter": []assert.JSONObject{{
						"os":           "linux",
						"architecture": "arm64",
						"variant":      "v8",
					}},
					"replication": assert.JSONObject{
						"strategy": "on_first_use",
						"upstream": "registry.example.org",
					},
				},
			},
			ExpectStatus: http.StatusConflict,
			ExpectBody:   assert.StringData("peer account filter needs to match primary account filter: primary account [{\"architecture\":\"arm64\",\"os\":\"linux\",\"variant\":\"v8\"}], peer account [{\"architecture\":\"amd64\",\"os\":\"linux\"}] \n"),
		}.Check(t, s2.Handler)
	})
}
