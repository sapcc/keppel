// SPDX-FileCopyrightText: 2018-2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppelv1_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"

	keppelv1 "github.com/sapcc/keppel/internal/api/keppel"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestAccountsAPI(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler
	tr, tr0 := easypg.NewTracker(t, s.DB.Db)
	tr0.AssertEmpty()

	// test the /keppel/v1 endpoint
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1",
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"auth_driver": "unittest"},
	}.Check(t, h)

	// no accounts right now
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"accounts": []any{}},
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/first",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("no permission for keppel_account:first:view\n"),
	}.Check(t, h)

	// create an account (this request is executed twice to test idempotency)
	for _, pass := range []int{1, 2} {
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
					"metadata":       nil,
					"rbac_policies":  []assert.JSONObject{},
				},
			},
		}.Check(t, h)

		// only the first pass should generate an audit event
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

	// check that account shows up in GET...
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"accounts": []assert.JSONObject{{
				"name":           "first",
				"auth_tenant_id": "tenant1",
				"metadata":       nil,
				"rbac_policies":  []assert.JSONObject{},
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
				"metadata":       nil,
				"rbac_policies":  []assert.JSONObject{},
			},
		},
	}.Check(t, h)

	// ...but only when one has view permission on the correct tenant
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

	// create an account with RBAC policies and GC policies (this request is executed twice to test idempotency)
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
	tagPoliciesJSON := []assert.JSONObject{
		{
			"match_repository": "library/.*",
			"block_overwrite":  true,
		},
		{
			"match_repository": "library/alpine",
			"block_delete":     true,
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
					"tag_policies":   tagPoliciesJSON,
				},
			},
			ExpectStatus: http.StatusOK,
			ExpectBody: assert.JSONObject{
				"account": assert.JSONObject{
					"name":           "second",
					"auth_tenant_id": "tenant1",
					"gc_policies":    gcPoliciesJSON,
					"metadata":       nil,
					"rbac_policies":  rbacPoliciesJSON,
					"tag_policies":   tagPoliciesJSON,
				},
			},
		}.Check(t, h)

		// only the first pass should generate audit events
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
						Attachments: []cadf.Attachment{
							{
								Name:    "gc-policies",
								TypeURI: "mime:application/json",
								Content: toJSONVia[[]keppel.GCPolicy](gcPoliciesJSON),
							},
							{
								Name:    "rbac-policies",
								TypeURI: "mime:application/json",
								Content: toJSONVia[[]keppel.RBACPolicy](rbacPoliciesJSON),
							},
							{
								Name:    "tag-policies",
								TypeURI: "mime:application/json",
								Content: toJSONVia[[]keppel.TagPolicy](tagPoliciesJSON),
							},
						},
					},
				},
			)
		} else {
			s.Auditor.ExpectEvents(t /*, nothing */)
		}
	}

	// check that this account also shows up in GET
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
					"metadata":       nil,
					"rbac_policies":  []assert.JSONObject{},
				},
				{
					"name":           "second",
					"auth_tenant_id": "tenant1",
					"gc_policies":    gcPoliciesJSON,
					"metadata":       nil,
					"rbac_policies":  rbacPoliciesJSON,
					"tag_policies":   tagPoliciesJSON,
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
				"metadata":       nil,
				"rbac_policies":  rbacPoliciesJSON,
				"tag_policies":   tagPoliciesJSON,
			},
		},
	}.Check(t, h)
	tr.DBChanges().AssertEqual(`
		INSERT INTO accounts (name, auth_tenant_id) VALUES ('first', 'tenant1');
		INSERT INTO accounts (name, auth_tenant_id, gc_policies_json, rbac_policies_json, tag_policies_json) VALUES ('second', 'tenant1', '[{"match_repository":".*/database","except_repository":"archive/.*","time_constraint":{"on":"pushed_at","newer_than":{"value":10,"unit":"d"}},"action":"protect"},{"match_repository":".*","only_untagged":true,"action":"delete"}]', '[{"match_repository":"library/.*","permissions":["anonymous_pull"]},{"match_repository":"library/alpine","match_username":".*@tenant2","permissions":["pull","push"]}]', '[{"match_repository":"library/.*","block_overwrite":true},{"match_repository":"library/alpine","block_delete":true}]');
	`)

	// check editing of RBAC policies
	newRBACPoliciesJSON := []assert.JSONObject{
		// rbacPoliciesJSON[0] is deleted
		// rbacPoliciesJSON[1] is updated as follows:
		{
			"match_repository": "library/alpine",
			"match_username":   ".*@tenant2",
			"permissions":      []string{"pull"},
		},
		// this one is entirely new:
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
				"metadata":       nil,
				"rbac_policies":  newRBACPoliciesJSON,
			},
		},
	}.Check(t, h)
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
				Attachments: []cadf.Attachment{{
					Name:    "rbac-policies",
					TypeURI: "mime:application/json",
					Content: toJSONVia[[]keppel.RBACPolicy](newRBACPoliciesJSON),
				}},
			},
		},
	)

	// test setting up a validation policy
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
				"metadata":       nil,
				"rbac_policies":  newRBACPoliciesJSON,
				"validation": assert.JSONObject{
					"required_labels": []string{"foo", "bar"},
				},
			},
		},
	}.Check(t, h)

	// setting an empty validation policy should be equivalent to removing it
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
				"metadata":       nil,
				"rbac_policies":  newRBACPoliciesJSON,
			},
		},
	}.Check(t, h)

	// test POST /keppel/v1/:accounts/sublease success case (error cases are in
	// TestPutAccountErrorCases and TestGetPutAccountReplicationOnFirstUse)
	s.FD.NextSubleaseTokenSecretToIssue = "this-is-the-token"
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/keppel/v1/accounts/second/sublease",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,change:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"sublease_token": makeSubleaseToken("second", "registry.example.org", "this-is-the-token")},
	}.Check(t, h)
	tr.DBChanges().AssertEqual(`
		UPDATE accounts SET gc_policies_json = '[]', rbac_policies_json = '[{"match_repository":"library/alpine","match_username":".*@tenant2","permissions":["pull"]},{"match_repository":"library/alpine","match_username":".*@tenant3","permissions":["pull","delete"]}]', tag_policies_json = '[]' WHERE name = 'second';
	`)
}

func TestGetAccountsErrorCases(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler

	// test invalid authentication (response includes auth challenges since the
	// default auth scheme is bearer token auth)
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

func TestPutAccountRBACPolicyNormalization(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler

	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"rbac_policies": []assert.JSONObject{{
					"match_username":        "mallory",
					"permissions":           nil, // this gets normalized...
					"forbidden_permissions": []string{"push"},
				}},
			},
		},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"name":           "first",
				"auth_tenant_id": "tenant1",
				"metadata":       nil,
				"rbac_policies": []assert.JSONObject{{
					"match_username":        "mallory",
					"permissions":           []string{}, // ...to this
					"forbidden_permissions": []string{"push"},
				}},
			},
		},
	}.Check(t, h)
}

func TestPutAccountErrorCases(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler
	tr, tr0 := easypg.NewTracker(t, s.DB.Db)
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
				"metadata":       nil,
				"rbac_policies":  []assert.JSONObject{},
			},
		},
	}.Check(t, h)

	// test invalid inputs
	assert.HTTPRequest{
		Method:       "PUT",
		Path:         "/keppel/v1/accounts/second",
		Header:       map[string]string{"X-Test-Perms": "change:tenant1"},
		Body:         assert.StringData(`{"account":???}`),
		ExpectStatus: http.StatusBadRequest,
		ExpectBody:   assert.StringData("request body is not valid JSON: invalid character '?' looking for beginning of value\n"),
	}.Check(t, h)

	assert.HTTPRequest{
		Method:       "PUT",
		Path:         "/keppel/v1/accounts/second",
		Header:       map[string]string{"X-Test-Perms": "change:tenant1"},
		Body:         assert.StringData(`{"account":""}`),
		ExpectStatus: http.StatusBadRequest,
		ExpectBody:   assert.StringData("request body is not valid JSON: json: cannot unmarshal string into Go struct field .account of type keppel.Account\n"),
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
		Path:   "/keppel/v1/accounts/v1",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("account names that look like API versions (e.g. v1) are reserved for internal use\n"),
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

	// test invalid authentication/authorization
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
			// default auth is bearer token auth, so an auth challenge gets rendered
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

	// test rejection by federation driver (we test both user error and server
	// error to validate that they generate the correct respective HTTP status
	// codes)
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

	// test rejection by storage driver
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

	// test setting up invalid required_labels
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/second",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"validation": assert.JSONObject{
					"required_labels": []string{"foo,", ",bar"},
				},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("invalid label name: \"foo,\"\n"),
	}.Check(t, h)

	// test malformed GC policies
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

	// test malformed RBAC policies
	rbacPolicyTestcases := []struct {
		RBACPolicyJSON assert.JSONObject
		ErrorMessage   string
	}{
		// NOTE: Many testcases come in pairs where the problematic permission is
		// in `permissions` the first time and in `forbidden_permissions` the second time.
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_repository": "library/.+",
			},
			ErrorMessage: "RBAC policy must grant at least one permission",
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_repository": "library/.+",
				"permissions":      []string{"pull", "push", "foo"},
			},
			ErrorMessage: `"foo" is not a valid RBAC policy permission`,
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_repository":      "library/.+",
				"permissions":           []string{"pull"},
				"forbidden_permissions": []string{"push", "foo"},
			},
			ErrorMessage: `"foo" is not a valid RBAC policy permission`,
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_repository":      "library/.+",
				"permissions":           []string{"pull"},
				"forbidden_permissions": []string{"pull", "push"},
			},
			ErrorMessage: `"pull" cannot be granted and forbidden by the same RBAC policy`,
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"permissions": []string{"anonymous_pull"},
			},
			ErrorMessage: `RBAC policy must have at least one "match_..." attribute`,
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_repository": "library/.+",
				"match_username":   "foo",
				"permissions":      []string{"anonymous_pull"},
			},
			ErrorMessage: `RBAC policy with "anonymous_pull" or "anonymous_first_pull" may not have the "match_username" attribute`,
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_repository":      "library/.+",
				"match_username":        "foo",
				"forbidden_permissions": []string{"anonymous_pull"},
			},
			ErrorMessage: `RBAC policy with "anonymous_pull" or "anonymous_first_pull" may not have the "match_username" attribute`,
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_repository": "library/.+",
				"permissions":      []string{"pull"},
			},
			ErrorMessage: `RBAC policy with "pull" must have the "match_cidr" or "match_username" attribute`,
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_repository":      "library/.+",
				"forbidden_permissions": []string{"pull"},
			},
			ErrorMessage: `RBAC policy with "pull" must have the "match_cidr" or "match_username" attribute`,
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_repository": "library/.+",
				"permissions":      []string{"delete"},
			},
			ErrorMessage: `RBAC policy with "delete" must have the "match_username" attribute`,
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_repository":      "library/.+",
				"forbidden_permissions": []string{"delete"},
			},
			ErrorMessage: `RBAC policy with "delete" must have the "match_username" attribute`,
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_repository": "library/.+",
				"permissions":      []string{"push"},
			},
			ErrorMessage: `RBAC policy with "push" must also grant "pull"`,
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_cidr": "0.0.0.0/64",
			},
			ErrorMessage: `"0.0.0.0/64" is not a valid CIDR`,
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_cidr":       "0.0.0.0/0",
				"match_repository": "test*",
				"permissions":      []string{"pull"},
			},
			ErrorMessage: "0.0.0.0/0 cannot be used as CIDR because it matches everything",
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_repository": "library/.+",
				"match_username":   "foo",
				"permissions":      []string{"anonymous_first_pull"},
			},
			ErrorMessage: `RBAC policy with "anonymous_pull" or "anonymous_first_pull" may not have the "match_username" attribute`,
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_repository":      "library/.+",
				"match_username":        "foo",
				"forbidden_permissions": []string{"anonymous_first_pull"},
			},
			ErrorMessage: `RBAC policy with "anonymous_pull" or "anonymous_first_pull" may not have the "match_username" attribute`,
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_repository": "library/.+",
				"permissions":      []string{"anonymous_first_pull"},
			},
			ErrorMessage: `RBAC policy with "anonymous_first_pull" may only be for external replica accounts`,
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_repository":      "library/.+",
				"forbidden_permissions": []string{"anonymous_first_pull"},
			},
			ErrorMessage: `RBAC policy with "anonymous_first_pull" may only be for external replica accounts`,
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_repository": "*/library",
				"permissions":      []string{"anonymous_pull"},
			},
			ErrorMessage: "request body is not valid JSON: \"*/library\" is not a valid regexp: error parsing regexp: missing argument to repetition operator: `*`",
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_repository": "library/.+",
				"match_username":   "[a-z]++@tenant2",
				"permissions":      []string{"pull"},
			},
			ErrorMessage: "request body is not valid JSON: \"[a-z]++@tenant2\" is not a valid regexp: error parsing regexp: invalid nested repetition operator: `++`",
		},
	}
	for _, tc := range rbacPolicyTestcases {
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
					"rbac_policies":  []assert.JSONObject{tc.RBACPolicyJSON},
				},
			},
			ExpectStatus: expectedStatus,
			ExpectBody:   assert.StringData(tc.ErrorMessage + "\n"),
		}.Check(t, h)
	}

	// TODO: why is there a positive test in here?
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
				"metadata":       nil,
				"name":           "first",
				"rbac_policies": []assert.JSONObject{{
					"match_cidr":  "1.2.0.0/16",
					"permissions": []string{"pull"},
				}},
			},
		},
	}.Check(t, h)
	tr.DBChanges().AssertEqual(`
		INSERT INTO accounts (name, auth_tenant_id, rbac_policies_json) VALUES ('first', 'tenant1', '[{"match_cidr":"1.2.0.0/16","permissions":["pull"]}]');
	`)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/first",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"metadata":       nil,
				"name":           "first",
				"rbac_policies": []assert.JSONObject{{
					"match_cidr":  "1.2.0.0/16",
					"permissions": []string{"pull"},
				}},
			},
		},
	}.Check(t, h)

	// test unexpected platform filter
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
		ExpectStatus: http.StatusConflict,
		ExpectBody:   assert.StringData("cannot change platform filter on existing account\n"),
	}.Check(t, h)

	// test unexpected platform filter on new primary account
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/third",
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

	// test errors for sublease token issuance: missing authentication/authorization
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/keppel/v1/accounts/first/sublease",
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("no bearer token found in request headers\n"),
		ExpectHeader: map[string]string{
			// default auth is bearer token auth, so an auth challenge gets rendered
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
		Path:         "/keppel/v1/accounts/unknown/sublease", // account does not exist
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,change:tenant1"},
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("no permission for keppel_account:unknown:change\n"),
	}.Check(t, h)

	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"in_maintenance": true, // this field used to be supported, but support for it was removed
			},
		},
		ExpectStatus: http.StatusBadRequest,
		ExpectBody:   assert.StringData("request body is not valid JSON: json: unknown field \"in_maintenance\"\n"),
	}.Check(t, h)

	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"metadata": assert.JSONObject{
					"foo": "bar",
				},
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("malformed attribute \"account.metadata\" in request body does no longer exist\n"),
	}.Check(t, h)

	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"name":           "first",
			},
		},
		ExpectStatus: http.StatusOK,
	}.Check(t, h)

	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
				"name":           "second",
			},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("changing attribute \"account.name\" in request body is not allowed\n"),
	}.Check(t, h)

	// test protection for managed accounts
	test.MustExec(t, s.DB, "UPDATE accounts SET is_managed = TRUE WHERE name = $1", "first")
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
			},
		},
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("cannot manually change configuration of a managed account\n"),
	}.Check(t, h)
	test.MustExec(t, s.DB, "UPDATE accounts SET is_managed = FALSE WHERE name = $1", "first")
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
					"metadata":       nil,
					"rbac_policies":  []assert.JSONObject{},
				},
			},
		}.Check(t, s1.Handler)

		// test error cases on creation
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/accounts/first",
			Header: map[string]string{"X-Test-Perms": "change:tenant1"},
			Body: assert.JSONObject{
				"account": assert.JSONObject{
					"auth_tenant_id": "tenant1",
					"replication":    assert.JSONObject{"strategy": "yes_please", "upstream": "registry.example.org"},
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
				"X-Test-Perms":          "change:tenant1",
				keppelv1.SubleaseHeader: makeSubleaseToken("first", "registry.example.org", "not-the-valid-token"),
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

		// test PUT success case
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/accounts/first",
			Header: map[string]string{
				"X-Test-Perms":          "change:tenant1",
				keppelv1.SubleaseHeader: makeSubleaseToken("first", "registry.example.org", "valid-token"),
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
					"metadata":       nil,
					"rbac_policies":  []assert.JSONObject{},
					"replication": assert.JSONObject{
						"strategy": "on_first_use",
						"upstream": "registry.example.org",
					},
				},
			},
		}.Check(t, s2.Handler)

		// PUT on existing account with replication unspecified is okay, leaves
		// replication settings unchanged
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
					"metadata":       nil,
					"rbac_policies":  []assert.JSONObject{},
					"replication": assert.JSONObject{
						"strategy": "on_first_use",
						"upstream": "registry.example.org",
					},
				},
			},
		}.Check(t, s2.Handler)

		// cannot issue sublease token for replica account (only for primary accounts)
		assert.HTTPRequest{
			Method:       "POST",
			Path:         "/keppel/v1/accounts/first/sublease",
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,change:tenant1"},
			ExpectStatus: http.StatusBadRequest,
			ExpectBody:   assert.StringData("operation not allowed for replica accounts\n"),
		}.Check(t, s2.Handler)

		// PUT on existing account with different replication settings is not allowed
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
					"metadata":       nil,
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

	// test error cases on creation
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
		ExpectBody:   assert.StringData("request body is not valid JSON: json: cannot unmarshal string into Go struct field Account.account.replication of type keppel.ReplicationExternalPeerSpec\n"),
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

	// test PUT success case
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
				"metadata":       nil,
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

	// PUT on existing account with replication unspecified is okay, leaves
	// replication settings unchanged
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
				"metadata":       nil,
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

	// test PUT on existing account to update replication credentials
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
				"metadata":       nil,
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

	// PUT on existing account with replication credentials section copied from
	// GET is okay, leaves replication settings unchanged too (this is important
	// because, in practice, clients copy the account config from GET, change a
	// thing, and PUT the result)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
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
				"metadata":       nil,
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

	// ...but changing the username without also supplying a password is wrong
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{
				"auth_tenant_id": "tenant1",
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

	// test sublease token issuance on account (external replicas count as primary
	// accounts for the purposes of account name subleasing)
	s.FD.NextSubleaseTokenSecretToIssue = "this-is-the-token"
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/keppel/v1/accounts/first/sublease",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,change:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"sublease_token": makeSubleaseToken("first", "registry.example.org", "this-is-the-token")},
	}.Check(t, h)

	// PUT on existing account with different replication settings is not allowed
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
				"metadata":       nil,
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

	// PUT on existing account with different platform filter is not allowed
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

func TestDeleteAccount(t *testing.T) {
	s := test.NewSetup(t,
		test.WithKeppelAPI,
		test.WithAccount(models.Account{Name: "test1", AuthTenantID: "tenant1"}),
	)
	h := s.Handler

	tr, tr0 := easypg.NewTracker(t, s.DB.Db)
	tr0.Ignore()

	// failure case: insufficient permissions (the "delete" permission refers to
	// manifests within the account, not the account itself)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
		ExpectStatus: http.StatusForbidden,
	}.Check(t, h)

	// DELETE on account should immediately mark it for deletion
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,change:tenant1"},
		ExpectStatus: http.StatusNoContent,
	}.Check(t, h)

	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET is_deleting = TRUE, next_deletion_attempt_at = %[1]d WHERE name = 'test1';
		`,
		s.Clock.Now().Unix(),
	)
	s.Auditor.ExpectEvents(t,
		cadf.Event{
			RequestPath: "/keppel/v1/accounts/test1",
			Action:      cadf.DeleteAction,
			Outcome:     "success",
			Reason:      test.CADFReasonOK,
			Target: cadf.Resource{
				TypeURI:   "docker-registry/account",
				ID:        "test1",
				ProjectID: "tenant1",
			},
		},
	)

	// account is already set to be deleted, so nothing happens
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,change:tenant1"},
		ExpectStatus: http.StatusNoContent,
	}.Check(t, h)

	tr.DBChanges().AssertEmpty()
	s.Auditor.ExpectEvents(t /*, nothing */)
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

func toJSONVia[T any](in any) string {
	// This is mostly the same as `test.ToJSON(in)`, but deserializes into
	// T in an intermediate step to render the JSON with the correct field order.
	// Used for the GCPolicy, RBACPolicy and SecurityScanPolicy audit event matches.
	buf, _ := json.Marshal(in)
	var intermediate T
	err := json.Unmarshal(buf, &intermediate)
	if err != nil {
		panic(err.Error())
	}
	buf, err = json.Marshal(intermediate)
	if err != nil {
		panic(err.Error())
	}
	return string(buf)
}

func deepCopyViaJSON[T any](in T) (out T) {
	buf, err := json.Marshal(in)
	if err != nil {
		panic(err.Error())
	}
	err = json.Unmarshal(buf, &out)
	if err != nil {
		panic(err.Error())
	}
	return out
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
		for _, name := range []models.AccountName{"first", "second", "third"} {
			assert.HTTPRequest{
				Method: "PUT",
				Path:   "/keppel/v1/accounts/" + string(name),
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
						"metadata":       nil,
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

		// create an account which inherits the PlatformFilter
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/accounts/first",
			Header: map[string]string{
				"X-Test-Perms":          "change:tenant1",
				keppelv1.SubleaseHeader: makeSubleaseToken("first", "registry.example.org", "valid-token"),
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
					"metadata":        nil,
					"platform_filter": testPlatformFilter,
					"rbac_policies":   []assert.JSONObject{},
					"replication": assert.JSONObject{
						"strategy": "on_first_use",
						"upstream": "registry.example.org",
					},
				},
			},
		}.Check(t, s2.Handler)

		// create an account with the same PlatformFilter
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/accounts/second",
			Header: map[string]string{
				"X-Test-Perms":          "change:tenant1",
				keppelv1.SubleaseHeader: makeSubleaseToken("second", "registry.example.org", "valid-token"),
			},
			Body: assert.JSONObject{
				"account": assert.JSONObject{
					"auth_tenant_id": "tenant1",
					"metadata":       nil,
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
					"metadata":        nil,
					"platform_filter": testPlatformFilter,
					"rbac_policies":   []assert.JSONObject{},
					"replication": assert.JSONObject{
						"strategy": "on_first_use",
						"upstream": "registry.example.org",
					},
				},
			},
		}.Check(t, s2.Handler)

		// create an account with an incompatible PlatformFilter
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/accounts/third",
			Header: map[string]string{
				"X-Test-Perms":          "change:tenant1",
				keppelv1.SubleaseHeader: makeSubleaseToken("third", "registry.example.org", "valid-token"),
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
			ExpectBody:   assert.StringData("peer account filter needs to match primary account filter: local account [{\"architecture\":\"arm64\",\"os\":\"linux\",\"variant\":\"v8\"}], peer account [{\"architecture\":\"amd64\",\"os\":\"linux\"}] \n"),
		}.Check(t, s2.Handler)
	})
}

func TestSecurityScanPoliciesHappyPath(t *testing.T) {
	s := test.NewSetup(t,
		test.WithKeppelAPI,
	)

	// we need to set test.AuthDriver.ExpectedUserName because this username is
	// matched against the managed_by_user field of our policies
	s.AD.ExpectedUserName = "exampleuser"

	// create a fresh account for testing
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{
			"account": assert.JSONObject{"auth_tenant_id": "tenant1"},
		},
		ExpectStatus: http.StatusOK,
	}.Check(t, s.Handler)

	// a freshly-created account should have no policies at all
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/first/security_scan_policies",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"policies": []assert.JSONObject{}},
	}.Check(t, s.Handler)
	s.Auditor.IgnoreEventsUntilNow()

	// helper function for testing a successful PUT of policies, followed by a GET
	// that returns those same policies
	expectPoliciesToBeApplied := func(policies ...assert.JSONObject) {
		assert.HTTPRequest{
			Method:       "PUT",
			Path:         "/keppel/v1/accounts/first/security_scan_policies",
			Header:       map[string]string{"X-Test-Perms": "change:tenant1"},
			Body:         assert.JSONObject{"policies": policies},
			ExpectStatus: http.StatusOK,
			ExpectBody:   assert.JSONObject{"policies": policies},
		}.Check(t, s.Handler)
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/keppel/v1/accounts/first/security_scan_policies",
			Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
			ExpectStatus: http.StatusOK,
			ExpectBody:   assert.JSONObject{"policies": policies},
		}.Check(t, s.Handler)
	}

	// PUT with no policies is okay, does nothing
	expectPoliciesToBeApplied( /* nothing */ )
	s.Auditor.ExpectEvents(t /*, nothing */)

	// add the policies from the API spec example
	policy1 := assert.JSONObject{
		"match_repository":       ".*",
		"match_vulnerability_id": ".*",
		"except_fix_released":    true,
		"action": assert.JSONObject{
			"ignore":     true,
			"assessment": "risk accepted: vulnerabilities without an available fix are not actionable",
		},
	}
	policy2 := assert.JSONObject{
		"managed_by_user":        "exampleuser",
		"match_repository":       "my-python-app|my-other-image",
		"match_vulnerability_id": "CVE-2022-40897",
		"action": assert.JSONObject{
			"severity":   "Low",
			"assessment": "adjusted severity: python-setuptools cannot be invoked through user requests",
		},
	}
	expectPoliciesToBeApplied(policy1, policy2)

	// adding two policies generates one create event per policy
	expectedEventForPolicy := func(action cadf.Action, policy assert.JSONObject) cadf.Event {
		return cadf.Event{
			RequestPath: "/keppel/v1/accounts/first/security_scan_policies",
			Action:      action,
			Outcome:     "success",
			Reason:      test.CADFReasonOK,
			Target: cadf.Resource{
				TypeURI:   "docker-registry/account",
				ID:        "first",
				ProjectID: "tenant1",
				Attachments: []cadf.Attachment{{
					Name:    "payload",
					TypeURI: "mime:application/json",
					Content: toJSONVia[keppel.SecurityScanPolicy](policy),
				}},
			},
		}
	}
	s.Auditor.ExpectEvents(t,
		expectedEventForPolicy("create/security-scan-policy", policy1),
		expectedEventForPolicy("create/security-scan-policy", policy2),
	)

	// update a policy -> one deletion event, one creation event
	policy1New := deepCopyViaJSON(policy1)
	policy1New["match_repository"] = "foo.*"
	expectPoliciesToBeApplied(policy1New, policy2)
	s.Auditor.ExpectEvents(t,
		expectedEventForPolicy("create/security-scan-policy", policy1New),
		expectedEventForPolicy("delete/security-scan-policy", policy1),
	)

	// update a policy managed by the current user -> same behavior
	policy2New := deepCopyViaJSON(policy2)
	policy2New["action"].(map[string]any)["severity"] = "Medium"
	expectPoliciesToBeApplied(policy1New, policy2New)
	s.Auditor.ExpectEvents(t,
		expectedEventForPolicy("create/security-scan-policy", policy2New),
		expectedEventForPolicy("delete/security-scan-policy", policy2),
	)

	// test deleting all policies
	expectPoliciesToBeApplied( /* nothing */ )
	s.Auditor.ExpectEvents(t,
		expectedEventForPolicy("delete/security-scan-policy", policy1New),
		expectedEventForPolicy("delete/security-scan-policy", policy2New),
	)

	// test expansion of "$REQUESTER" in "managed_by_user" field (this cannot use
	// expectPoliciesToBeApplied() since the response body is different from the
	// request body)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first/security_scan_policies",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{"policies": []assert.JSONObject{{
			"managed_by_user":        "$REQUESTER",
			"match_repository":       ".*",
			"match_vulnerability_id": ".*",
			"except_fix_released":    true,
			"action": assert.JSONObject{
				"ignore":     true,
				"assessment": "risk accepted: vulnerabilities without an available fix are not actionable",
			}},
		}},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{"policies": []assert.JSONObject{{
			"managed_by_user":        "exampleuser",
			"match_repository":       ".*",
			"match_vulnerability_id": ".*",
			"except_fix_released":    true,
			"action": assert.JSONObject{
				"ignore":     true,
				"assessment": "risk accepted: vulnerabilities without an available fix are not actionable",
			}},
		}},
	}.Check(t, s.Handler)
	s.Auditor.IgnoreEventsUntilNow()
}

func TestSecurityScanPoliciesValidationErrors(t *testing.T) {
	s := test.NewSetup(t,
		test.WithKeppelAPI,
		test.WithAccount(models.Account{Name: "first", AuthTenantID: "tenant1"}),
	)

	// we need to set test.AuthDriver.ExpectedUserName because this username is
	// matched against the managed_by_user field of our policies
	s.AD.ExpectedUserName = "exampleuser"

	// check unmarshalling errors
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first/security_scan_policies",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{"policies": []assert.JSONObject{
			{
				"match_repository":       ".*",
				"match_vulnerability_id": ".*",
				"action": assert.JSONObject{
					"severity":   "Low",
					"assessment": "not important",
				},
				"unknown_field": 42,
			},
		}},
		ExpectStatus: http.StatusBadRequest,
		ExpectBody:   assert.StringData("request body is not valid JSON: json: unknown field \"unknown_field\"\n"),
	}.Check(t, s.Handler)

	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first/security_scan_policies",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{"policies": []assert.JSONObject{
			{
				"match_repository":       ".*",
				"match_vulnerability_id": "(.*",
				"action": assert.JSONObject{
					"severity":   "Low",
					"assessment": "not important",
				},
			},
		}},
		ExpectStatus: http.StatusBadRequest,
		ExpectBody:   assert.StringData("request body is not valid JSON: \"(.*\" is not a valid regexp: error parsing regexp: missing closing ): `^(?:(.*)$`\n"),
	}.Check(t, s.Handler)

	// check all policy-local validations (every policy has exactly one error)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/accounts/first/security_scan_policies",
		Header: map[string]string{"X-Test-Perms": "change:tenant1"},
		Body: assert.JSONObject{"policies": []assert.JSONObject{
			{
				// missing "match_repository"
				"match_vulnerability_id": ".*",
				"action": assert.JSONObject{
					"severity":   "Low",
					"assessment": "not important",
				},
			},
			{
				// missing "match_vulnerability"
				"match_repository": ".*",
				"action": assert.JSONObject{
					"severity":   "Low",
					"assessment": "not important",
				},
			},
			{
				// missing "assessment"
				"match_repository":       ".*",
				"match_vulnerability_id": ".*",
				"action": assert.JSONObject{
					"severity": "Low",
				},
			},
			{
				// overlong "assessment"
				"match_repository":       ".*",
				"match_vulnerability_id": ".*",
				"action": assert.JSONObject{
					"severity":   "Low",
					"assessment": strings.Repeat("a", 1025),
				},
			},
			{
				// both "severity" and "ignore"
				"match_repository":       ".*",
				"match_vulnerability_id": ".*",
				"action": assert.JSONObject{
					"severity":   "Clean",
					"ignore":     true,
					"assessment": "not important",
				},
			},
			{
				// neither "severity" nor "ignore"
				"match_repository":       ".*",
				"match_vulnerability_id": ".*",
				"action": assert.JSONObject{
					"assessment": "not important",
				},
			},
			{
				// unknown value for "severity"
				"match_repository":       ".*",
				"match_vulnerability_id": ".*",
				"action": assert.JSONObject{
					"severity":   "Pending",
					"assessment": "not important",
				},
			},
			{
				// unacceptable value for "severity" (must be an explicit value)
				"match_repository":       ".*",
				"match_vulnerability_id": ".*",
				"action": assert.JSONObject{
					"severity":   "Unknown",
					"assessment": "not important",
				},
			},
		}},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody: assert.StringData(strings.Join([]string{
			`policies[0] must have the "match_repository" attribute`,
			`policies[1] must have the "match_vulnerability_id" attribute`,
			`policies[2].action must have the "assessment" attribute`,
			`policies[3].action.assessment cannot be larger than 1 KiB`,
			`policies[4].action cannot have the "severity" attribute when "ignore" is set`,
			`policies[5].action must have the "severity" attribute when "ignore" is not set`,
			`policies[6].action.severity contains the invalid value "Pending"`,
			`policies[7].action.severity contains the invalid value "Unknown"`,
		}, "\n") + "\n"),
	}.Check(t, s.Handler)

	// t.Error("TODO: fail on missing auth, fail on all validations in PUT")
}

func TestSecurityScanPoliciesAuthorizationErrors(t *testing.T) {
	s := test.NewSetup(t,
		test.WithKeppelAPI,
		test.WithAccount(models.Account{Name: "first", AuthTenantID: "tenant1"}),
	)

	// we need to set test.AuthDriver.ExpectedUserName because this username is
	// matched against the managed_by_user field of our policies
	s.AD.ExpectedUserName = "exampleuser"

	// PUT requires CanChangeAccount
	assert.HTTPRequest{
		Method:       "PUT",
		Path:         "/keppel/v1/accounts/first/security_scan_policies",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		Body:         assert.JSONObject{"policies": []assert.JSONObject{}},
		ExpectStatus: http.StatusForbidden,
	}.Check(t, s.Handler)

	// we should not be allowed to put in policies under a different user's name
	foreignPolicy := assert.JSONObject{
		"managed_by_user":        "johndoe",
		"match_repository":       ".*",
		"match_vulnerability_id": ".*",
		"action": assert.JSONObject{
			"assessment": "not important",
			"severity":   "Low",
		},
	}
	foreignPolicyJSON := toJSONVia[keppel.SecurityScanPolicy](foreignPolicy)
	assert.HTTPRequest{
		Method:       "PUT",
		Path:         "/keppel/v1/accounts/first/security_scan_policies",
		Header:       map[string]string{"X-Test-Perms": "change:tenant1"},
		Body:         assert.JSONObject{"policies": []assert.JSONObject{foreignPolicy}},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody: assert.StringData(
			fmt.Sprintf("cannot apply this new or updated policy that is managed by a different user: %s\n", foreignPolicyJSON),
		),
	}.Check(t, s.Handler)

	// as preparation for the next test, put in a pre-existing policy managed by a
	// different user
	test.MustExec(t, s.DB, `UPDATE accounts SET security_scan_policies_json = $1`,
		fmt.Sprintf("[%s]", foreignPolicyJSON))

	// it's okay if we leave that policy untouched...
	assert.HTTPRequest{
		Method:       "PUT",
		Path:         "/keppel/v1/accounts/first/security_scan_policies",
		Header:       map[string]string{"X-Test-Perms": "change:tenant1"},
		Body:         assert.JSONObject{"policies": []assert.JSONObject{foreignPolicy}},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"policies": []assert.JSONObject{foreignPolicy}},
	}.Check(t, s.Handler)

	// ...but updating is not okay...
	delete(foreignPolicy, "managed_by_user")
	foreignPolicy["match_repository"] = "definitely-not-the-old-value"
	assert.HTTPRequest{
		Method:       "PUT",
		Path:         "/keppel/v1/accounts/first/security_scan_policies",
		Header:       map[string]string{"X-Test-Perms": "change:tenant1"},
		Body:         assert.JSONObject{"policies": []assert.JSONObject{foreignPolicy}},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody: assert.StringData(
			fmt.Sprintf("cannot update or delete this existing policy that is managed by a different user: %s\n", foreignPolicyJSON),
		),
	}.Check(t, s.Handler)

	// ...and deleting is also not okay
	assert.HTTPRequest{
		Method:       "PUT",
		Path:         "/keppel/v1/accounts/first/security_scan_policies",
		Header:       map[string]string{"X-Test-Perms": "change:tenant1"},
		Body:         assert.JSONObject{"policies": []assert.JSONObject{}},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody: assert.StringData(
			fmt.Sprintf("cannot update or delete this existing policy that is managed by a different user: %s\n", foreignPolicyJSON),
		),
	}.Check(t, s.Handler)
}
