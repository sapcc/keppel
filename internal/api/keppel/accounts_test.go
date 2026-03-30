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

	"github.com/majewsky/gg/jsonmatch"
	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/httptest"

	keppelv1 "github.com/sapcc/keppel/internal/api/keppel"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestAccountsAPI(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler
	ctx := t.Context()
	tr, tr0 := easypg.NewTracker(t, s.DB.Db)
	tr0.AssertEmpty()

	// test the /keppel/v1 endpoint
	h.RespondTo(ctx, "GET /keppel/v1").
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{"auth_driver": "unittest"})

	// no accounts right now
	h.RespondTo(ctx, "GET /keppel/v1/accounts", withPerms("view:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"accounts": []jsonmatch.Object{},
		})
	h.RespondTo(ctx, "GET /keppel/v1/accounts/first", withPerms("view:tenant1")).
		ExpectText(t, http.StatusForbidden, "no permission for keppel_account:first:view\n")

	// create an account (this request is executed twice to test idempotency)
	for _, pass := range []int{1, 2} {
		h.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
			withPerms("change:tenant1"),
			httptest.WithJSONBody(map[string]any{
				"account": map[string]any{
					"auth_tenant_id": "tenant1",
				},
			}),
		).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"account": jsonmatch.Object{
				"name":           "first",
				"auth_tenant_id": "tenant1",
				"metadata":       nil,
				"rbac_policies":  []jsonmatch.Object{},
			},
		})

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
	h.RespondTo(ctx, "GET /keppel/v1/accounts", withPerms("view:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"accounts": []jsonmatch.Object{{
				"name":           "first",
				"auth_tenant_id": "tenant1",
				"metadata":       nil,
				"rbac_policies":  []jsonmatch.Object{},
			}},
		})
	h.RespondTo(ctx, "GET /keppel/v1/accounts/first", withPerms("view:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"account": jsonmatch.Object{
				"name":           "first",
				"auth_tenant_id": "tenant1",
				"metadata":       nil,
				"rbac_policies":  []jsonmatch.Object{},
			},
		})

	// ...but only when one has view permission on the correct tenant
	h.RespondTo(ctx, "GET /keppel/v1/accounts", withPerms("view:tenant2")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"accounts": []jsonmatch.Object{},
		})
	h.RespondTo(ctx, "GET /keppel/v1/accounts/first", withPerms("view:tenant2")).
		ExpectText(t, http.StatusForbidden, "no permission for keppel_account:first:view\n")

	// create an account with RBAC policies and GC policies (this request is executed twice to test idempotency)
	gcPoliciesJSON := []jsonmatch.Object{
		{
			"match_repository":  ".*/database",
			"except_repository": "archive/.*",
			"time_constraint": jsonmatch.Object{
				"on": "pushed_at",
				"newer_than": jsonmatch.Object{
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
	rbacPoliciesJSON := []jsonmatch.Object{
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
	tagPoliciesJSON := []jsonmatch.Object{
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
		h.RespondTo(ctx, "PUT /keppel/v1/accounts/second",
			withPerms("change:tenant1"),
			httptest.WithJSONBody(map[string]any{
				"account": map[string]any{
					"auth_tenant_id": "tenant1",
					"gc_policies":    gcPoliciesJSON,
					"rbac_policies":  rbacPoliciesJSON,
					"tag_policies":   tagPoliciesJSON,
				},
			}),
		).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"account": jsonmatch.Object{
				"name":           "second",
				"auth_tenant_id": "tenant1",
				"gc_policies":    gcPoliciesJSON,
				"metadata":       nil,
				"rbac_policies":  rbacPoliciesJSON,
				"tag_policies":   tagPoliciesJSON,
			},
		})

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
	h.RespondTo(ctx, "GET /keppel/v1/accounts", withPerms("view:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"accounts": []jsonmatch.Object{
				{
					"name":           "first",
					"auth_tenant_id": "tenant1",
					"metadata":       nil,
					"rbac_policies":  []jsonmatch.Object{},
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
		})
	h.RespondTo(ctx, "GET /keppel/v1/accounts/second", withPerms("view:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"account": jsonmatch.Object{
				"name":           "second",
				"auth_tenant_id": "tenant1",
				"gc_policies":    gcPoliciesJSON,
				"metadata":       nil,
				"rbac_policies":  rbacPoliciesJSON,
				"tag_policies":   tagPoliciesJSON,
			},
		})
	tr.DBChanges().AssertEqual(`
		INSERT INTO accounts (name, auth_tenant_id) VALUES ('first', 'tenant1');
		INSERT INTO accounts (name, auth_tenant_id, gc_policies_json, rbac_policies_json, tag_policies_json) VALUES ('second', 'tenant1', '[{"match_repository":".*/database","except_repository":"archive/.*","time_constraint":{"on":"pushed_at","newer_than":{"value":10,"unit":"d"}},"action":"protect"},{"match_repository":".*","only_untagged":true,"action":"delete"}]', '[{"match_repository":"library/.*","permissions":["anonymous_pull"]},{"match_repository":"library/alpine","match_username":".*@tenant2","permissions":["pull","push"]}]', '[{"match_repository":"library/.*","block_overwrite":true},{"match_repository":"library/alpine","block_delete":true}]');
	`)

	// check editing of RBAC policies
	newRBACPoliciesJSON := []jsonmatch.Object{
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
	h.RespondTo(ctx, "PUT /keppel/v1/accounts/second",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
				"rbac_policies":  newRBACPoliciesJSON,
			},
		}),
	).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"account": jsonmatch.Object{
			"name":           "second",
			"auth_tenant_id": "tenant1",
			"metadata":       nil,
			"rbac_policies":  newRBACPoliciesJSON,
		},
	})
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

	// test POST /keppel/v1/:accounts/sublease success case (error cases are in
	// TestPutAccountErrorCases and TestGetPutAccountReplicationOnFirstUse)
	s.FD.NextSubleaseTokenSecretToIssue = "this-is-the-token"
	h.RespondTo(ctx, "POST /keppel/v1/accounts/second/sublease", withPerms("view:tenant1,change:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"sublease_token": makeSubleaseToken("second", "registry.example.org", "this-is-the-token"),
		})
	tr.DBChanges().AssertEqual(`
		UPDATE accounts SET gc_policies_json = '[]', rbac_policies_json = '[{"match_repository":"library/alpine","match_username":".*@tenant2","permissions":["pull"]},{"match_repository":"library/alpine","match_username":".*@tenant3","permissions":["pull","delete"]}]', tag_policies_json = '[]' WHERE name = 'second';
	`)
}

func TestAccountValidationPolicies(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler
	ctx := t.Context()
	_, tr0 := easypg.NewTracker(t, s.DB.Db)
	tr0.AssertEmpty()

	// shorthand for configuring the account "first" with a PUT request
	putAccount := func(accountConfig map[string]any) httptest.Response {
		return h.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
			withPerms("change:tenant1"),
			httptest.WithJSONBody(map[string]any{"account": accountConfig}),
		)
	}

	// Create account
	putAccount(map[string]any{
		"auth_tenant_id": "tenant1",
	}).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"account": jsonmatch.Object{
			"name":           "first",
			"auth_tenant_id": "tenant1",
			"metadata":       nil,
			"rbac_policies":  []jsonmatch.Object{},
		},
	})

	// Reject if rule_for_manifest is not a valid CEL expression
	putAccount(map[string]any{
		"auth_tenant_id": "tenant1",
		"rbac_policies":  []map[string]any{},
		"validation": map[string]any{
			"rule_for_manifest": "the labels foo and bar should be present",
		},
	}).ExpectStatus(t, http.StatusUnprocessableEntity)

	// Reject if rule_for_manifest does not evaluate to bool
	putAccount(map[string]any{
		"auth_tenant_id": "tenant1",
		"rbac_policies":  []map[string]any{},
		"validation": map[string]any{
			"rule_for_manifest": "'foo' in labels ? 1 : 0",
		},
	}).ExpectText(t, http.StatusUnprocessableEntity, "output of CEL expression must be bool but is \"int\"\n")

	// Reject if required_labels and rule_for_manifest are not logically equivalent
	putAccount(map[string]any{
		"auth_tenant_id": "tenant1",
		"rbac_policies":  []map[string]any{},
		"validation": map[string]any{
			"rule_for_manifest": "'foo' in labels && 'bar' in labels",
			"required_labels":   []string{"qux", "quux"},
		},
	}).ExpectText(t, http.StatusUnprocessableEntity, "required labels [\"qux\" \"quux\"] do not match rule for manifest \"'foo' in labels && 'bar' in labels\"\n")

	// Accept if only rule_for_manifest is provided
	putAccount(map[string]any{
		"auth_tenant_id": "tenant1",
		"rbac_policies":  []map[string]any{},
		"validation": map[string]any{
			"rule_for_manifest": "'foo' in labels && 'bar' in labels",
		},
	}).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"account": jsonmatch.Object{
			"name":           "first",
			"auth_tenant_id": "tenant1",
			"metadata":       nil,
			"rbac_policies":  []jsonmatch.Object{},
			"validation": jsonmatch.Object{
				"rule_for_manifest": "'foo' in labels && 'bar' in labels",
				"required_labels":   []string{"foo", "bar"},
			},
		},
	})

	// Reset if an empty rule_for_manifest is provided
	putAccount(map[string]any{
		"auth_tenant_id": "tenant1",
		"rbac_policies":  []map[string]any{},
		"validation": map[string]any{
			"rule_for_manifest": "",
		},
	}).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"account": jsonmatch.Object{
			"name":           "first",
			"auth_tenant_id": "tenant1",
			"metadata":       nil,
			"rbac_policies":  []jsonmatch.Object{},
		},
	})

	// Accept if only required_labels is provided
	putAccount(map[string]any{
		"auth_tenant_id": "tenant1",
		"rbac_policies":  []map[string]any{},
		"validation": map[string]any{
			"required_labels": []string{"baz", "qux"},
		},
	}).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"account": jsonmatch.Object{
			"name":           "first",
			"auth_tenant_id": "tenant1",
			"metadata":       nil,
			"rbac_policies":  []jsonmatch.Object{},
			"validation": jsonmatch.Object{
				"rule_for_manifest": "'baz' in labels && 'qux' in labels",
				"required_labels":   []string{"baz", "qux"},
			},
		},
	})

	// Accept if both required_labels and rule_for_manifest are provided and valid
	putAccount(map[string]any{
		"auth_tenant_id": "tenant1",
		"rbac_policies":  []map[string]any{},
		"validation": map[string]any{
			"rule_for_manifest": "'quux' in labels",
			"required_labels":   []string{"quux"},
		},
	}).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"account": jsonmatch.Object{
			"name":           "first",
			"auth_tenant_id": "tenant1",
			"metadata":       nil,
			"rbac_policies":  []jsonmatch.Object{},
			"validation": jsonmatch.Object{
				"rule_for_manifest": "'quux' in labels",
				"required_labels":   []string{"quux"},
			},
		},
	})

	// Setting an empty validation policy should be equivalent to removing it
	putAccount(map[string]any{
		"auth_tenant_id": "tenant1",
		"rbac_policies":  []map[string]any{},
		"validation": map[string]any{
			"required_labels":   []string{},
			"rule_for_manifest": "",
		},
	}).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"account": jsonmatch.Object{
			"name":           "first",
			"auth_tenant_id": "tenant1",
			"metadata":       nil,
			"rbac_policies":  []jsonmatch.Object{},
		},
	})
}

func TestGetAccountsErrorCases(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler
	ctx := t.Context()

	// test invalid authentication (response includes auth challenges since the
	// default auth scheme is bearer token auth)
	h.RespondTo(ctx, "GET /keppel/v1/accounts").
		ExpectText(t, http.StatusUnauthorized, "unauthorized\n")

	resp := h.RespondTo(ctx, "GET /keppel/v1/accounts/first")
	assert.Equal(t, resp.Header().Get("Www-Authenticate"),
		`Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="keppel_account:first:view"`)
	resp.ExpectText(t, http.StatusForbidden, "no bearer token found in request headers\n")

	resp = h.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
			},
		}))
	assert.Equal(t, resp.Header().Get("Www-Authenticate"),
		`Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="keppel_auth_tenant:tenant1:change"`)
	resp.ExpectText(t, http.StatusForbidden, "no bearer token found in request headers\n")
}

func TestPutAccountRBACPolicyNormalization(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler
	ctx := t.Context()

	h.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
				"rbac_policies": []map[string]any{{
					"match_username":        "mallory",
					"permissions":           nil, // this gets normalized...
					"forbidden_permissions": []string{"push"},
				}},
			},
		}),
	).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"account": jsonmatch.Object{
			"name":           "first",
			"auth_tenant_id": "tenant1",
			"metadata":       nil,
			"rbac_policies": []jsonmatch.Object{{
				"match_username":        "mallory",
				"permissions":           []string{}, // ...to this
				"forbidden_permissions": []string{"push"},
			}},
		},
	})
}

func TestPutAccountErrorCases(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler
	ctx := t.Context()
	tr, tr0 := easypg.NewTracker(t, s.DB.Db)
	tr0.AssertEmpty()

	//preparation: create an account (so that we can check the error that the requested account name is taken)
	h.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
			},
		}),
	).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"account": jsonmatch.Object{
			"name":           "first",
			"auth_tenant_id": "tenant1",
			"metadata":       nil,
			"rbac_policies":  []jsonmatch.Object{},
		},
	})

	// test invalid inputs
	h.RespondTo(ctx, "PUT /keppel/v1/accounts/second",
		withPerms("change:tenant1"),
		httptest.WithBody(strings.NewReader(`{"account":???}`)),
	).ExpectText(t, http.StatusBadRequest, "request body is not valid JSON: invalid character '?' looking for beginning of value\n")

	h.RespondTo(ctx, "PUT /keppel/v1/accounts/second",
		withPerms("change:tenant1"),
		httptest.WithBody(strings.NewReader(`{"account":""}`)),
	).ExpectText(t, http.StatusBadRequest, "request body is not valid JSON: json: cannot unmarshal string into Go struct field .account of type keppel.Account\n")

	h.RespondTo(ctx, "PUT /keppel/v1/accounts/keppel-api",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
			},
		})).ExpectText(t, http.StatusUnprocessableEntity, "account names with the prefix \"keppel\" are reserved for internal use\n")

	h.RespondTo(ctx, "PUT /keppel/v1/accounts/v1",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
			},
		})).ExpectText(t, http.StatusUnprocessableEntity, "account names that look like API versions (e.g. v1) are reserved for internal use\n")

	// Just to be sure that this does not regress with any refactors in the future
	for _, accountName := range []string{"_blobs", "_chunks", "-invalid"} {
		h.RespondTo(ctx, "PUT /keppel/v1/accounts/"+accountName,
			withPerms("change:tenant1"),
			httptest.WithJSONBody(map[string]any{
				"account": map[string]any{
					"auth_tenant_id": "tenant1",
				},
			})).ExpectText(t, http.StatusNotFound, "404 page not found\n")
		// ^ The API route handler uses `[a-z0-9][a-z0-9-]{0,47}`, so we expect a 404 here.
	}

	h.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
		withPerms("change:tenant2"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant2",
			},
		})).ExpectText(t, http.StatusConflict, "account name already in use by a different tenant\n")

	// test invalid authentication/authorization
	resp := h.RespondTo(ctx, "PUT /keppel/v1/accounts/second",
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
			},
		}))
	assert.Equal(t, resp.Header().Get("Www-Authenticate"),
		`Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="keppel_auth_tenant:tenant1:change"`)
	resp.ExpectText(t, http.StatusForbidden, "no bearer token found in request headers\n")

	h.RespondTo(ctx, "PUT /keppel/v1/accounts/second",
		withPerms("view:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
			},
		})).ExpectText(t, http.StatusForbidden, "no permission for keppel_auth_tenant:tenant1:change\n")

	// test rejection by federation driver (we test both user error and server
	// error to validate that they generate the correct respective HTTP status
	// codes)
	s.FD.ClaimFailsBecauseOfUserError = true
	h.RespondTo(ctx, "PUT /keppel/v1/accounts/second",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
			},
		})).ExpectText(t, http.StatusForbidden, "cannot assign name \"second\" to auth tenant \"tenant1\"\n")
	s.FD.ClaimFailsBecauseOfUserError = false

	s.FD.ClaimFailsBecauseOfServerError = true
	h.RespondTo(ctx, "PUT /keppel/v1/accounts/second",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
			},
		})).ExpectText(t, http.StatusInternalServerError, "failed to assign name \"second\" to auth tenant \"tenant1\"\n")
	s.FD.ClaimFailsBecauseOfServerError = false

	// test rejection by storage driver
	s.SD.ForbidNewAccounts = true
	h.RespondTo(ctx, "PUT /keppel/v1/accounts/second",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
			},
		})).ExpectText(t, http.StatusConflict, "cannot set up backing storage for this account: CanSetupAccount failed as requested\n")
	s.SD.ForbidNewAccounts = false

	// test setting up invalid required_labels
	h.RespondTo(ctx, "PUT /keppel/v1/accounts/second",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
				"validation": map[string]any{
					"required_labels": []string{"foo,", ",bar"},
				},
			},
		})).ExpectText(t, http.StatusUnprocessableEntity, "invalid label name: \"foo,\"\n")

	// test malformed GC policies
	gcPolicyTestcases := []struct {
		GCPolicyJSON map[string]any
		ErrorMessage string
	}{
		{
			GCPolicyJSON: map[string]any{
				"except_repository": "library/.*",
				"only_untagged":     true,
				"action":            "delete",
			},
			ErrorMessage: `GC policy must have the "match_repository" attribute`,
		},
		{
			GCPolicyJSON: map[string]any{
				"match_repository": "*/library",
				"only_untagged":    true,
				"action":           "delete",
			},
			ErrorMessage: "request body is not valid JSON: \"*/library\" is not a valid regexp: error parsing regexp: missing argument to repetition operator: `*`",
		},
		{
			GCPolicyJSON: map[string]any{
				"match_repository":  "library/.*",
				"except_repository": "*/library",
				"only_untagged":     true,
				"action":            "delete",
			},
			ErrorMessage: "request body is not valid JSON: \"*/library\" is not a valid regexp: error parsing regexp: missing argument to repetition operator: `*`",
		},
		{
			GCPolicyJSON: map[string]any{
				"match_repository": "library/.*",
				"only_untagged":    true,
			},
			ErrorMessage: `GC policy must have the "action" attribute`,
		},
		{
			GCPolicyJSON: map[string]any{
				"match_repository": "library/.*",
				"only_untagged":    true,
				"action":           "foo",
			},
			ErrorMessage: `"foo" is not a valid action for a GC policy`,
		},
		{
			GCPolicyJSON: map[string]any{
				"match_repository": "library/.*",
				"match_tag":        "*-foo",
				"action":           "delete",
			},
			ErrorMessage: "request body is not valid JSON: \"*-foo\" is not a valid regexp: error parsing regexp: missing argument to repetition operator: `*`",
		},
		{
			GCPolicyJSON: map[string]any{
				"match_repository": "library/.*",
				"match_tag":        "foo-.*",
				"except_tag":       "*-bar",
				"action":           "delete",
			},
			ErrorMessage: "request body is not valid JSON: \"*-bar\" is not a valid regexp: error parsing regexp: missing argument to repetition operator: `*`",
		},
		{
			GCPolicyJSON: map[string]any{
				"match_repository": "library/.*",
				"match_tag":        "foo-.*",
				"only_untagged":    true,
				"action":           "delete",
			},
			ErrorMessage: `GC policy cannot have the "match_tag" attribute when "only_untagged" is set`,
		},
		{
			GCPolicyJSON: map[string]any{
				"match_repository": "library/.*",
				"except_tag":       "foo-.*",
				"only_untagged":    true,
				"action":           "delete",
			},
			ErrorMessage: `GC policy cannot have the "except_tag" attribute when "only_untagged" is set`,
		},
		{
			GCPolicyJSON: map[string]any{
				"match_repository": "library/.*",
				"only_untagged":    true,
				"time_constraint":  map[string]any{},
				"action":           "delete",
			},
			ErrorMessage: `GC policy time constraint must have the "on" attribute`,
		},
		{
			GCPolicyJSON: map[string]any{
				"match_repository": "library/.*",
				"only_untagged":    true,
				"time_constraint": map[string]any{
					"on":         "frobnicated_at",
					"newer_than": map[string]any{"value": 10, "unit": "d"},
				},
				"action": "delete",
			},
			ErrorMessage: `"frobnicated_at" is not a valid target for a GC policy time constraint`,
		},
		{
			GCPolicyJSON: map[string]any{
				"match_repository": "library/.*",
				"only_untagged":    true,
				"time_constraint": map[string]any{
					"on": "last_pulled_at",
				},
				"action": "delete",
			},
			ErrorMessage: `GC policy time constraint needs to set at least one attribute other than "on"`,
		},
		{
			GCPolicyJSON: map[string]any{
				"match_repository": "library/.*",
				"only_untagged":    true,
				"time_constraint": map[string]any{
					"on":         "pushed_at",
					"oldest":     10,
					"older_than": map[string]any{"value": 5, "unit": "h"},
				},
				"action": "protect",
			},
			ErrorMessage: `GC policy time constraint cannot set all these attributes at once: "oldest", "older_than"`,
		},
		{
			GCPolicyJSON: map[string]any{
				"match_repository": "library/.*",
				"only_untagged":    true,
				"time_constraint": map[string]any{
					"on":     "pushed_at",
					"oldest": 10,
				},
				"action": "delete",
			},
			ErrorMessage: `GC policy with action "delete" cannot set the "time_constraint.oldest" attribute`,
		},
		{
			GCPolicyJSON: map[string]any{
				"match_repository": "library/.*",
				"only_untagged":    true,
				"time_constraint": map[string]any{
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

		h.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
			withPerms("change:tenant1"),
			httptest.WithJSONBody(map[string]any{
				"account": map[string]any{
					"auth_tenant_id": "tenant1",
					"gc_policies":    []map[string]any{tc.GCPolicyJSON},
				},
			})).ExpectText(t, expectedStatus, tc.ErrorMessage+"\n")
	}

	// test malformed RBAC policies
	rbacPolicyTestcases := []struct {
		RBACPolicyJSON map[string]any
		ErrorMessage   string
	}{
		// NOTE: Many testcases come in pairs where the problematic permission is
		// in `permissions` the first time and in `forbidden_permissions` the second time.
		{
			RBACPolicyJSON: map[string]any{
				"match_repository": "library/.+",
			},
			ErrorMessage: "RBAC policy must grant at least one permission",
		},
		{
			RBACPolicyJSON: map[string]any{
				"match_repository": "library/.+",
				"permissions":      []string{"pull", "push", "foo"},
			},
			ErrorMessage: `"foo" is not a valid RBAC policy permission`,
		},
		{
			RBACPolicyJSON: map[string]any{
				"match_repository":      "library/.+",
				"permissions":           []string{"pull"},
				"forbidden_permissions": []string{"push", "foo"},
			},
			ErrorMessage: `"foo" is not a valid RBAC policy permission`,
		},
		{
			RBACPolicyJSON: map[string]any{
				"match_repository":      "library/.+",
				"permissions":           []string{"pull"},
				"forbidden_permissions": []string{"pull", "push"},
			},
			ErrorMessage: `"pull" cannot be granted and forbidden by the same RBAC policy`,
		},
		{
			RBACPolicyJSON: map[string]any{
				"permissions": []string{"anonymous_pull"},
			},
			ErrorMessage: `RBAC policy must have at least one "match_..." attribute`,
		},
		{
			RBACPolicyJSON: map[string]any{
				"match_repository": "library/.+",
				"match_username":   "foo",
				"permissions":      []string{"anonymous_pull"},
			},
			ErrorMessage: `RBAC policy with "anonymous_pull" or "anonymous_first_pull" may not have the "match_username" attribute`,
		},
		{
			RBACPolicyJSON: map[string]any{
				"match_repository":      "library/.+",
				"match_username":        "foo",
				"forbidden_permissions": []string{"anonymous_pull"},
			},
			ErrorMessage: `RBAC policy with "anonymous_pull" or "anonymous_first_pull" may not have the "match_username" attribute`,
		},
		{
			RBACPolicyJSON: map[string]any{
				"match_repository": "library/.+",
				"permissions":      []string{"pull"},
			},
			ErrorMessage: `RBAC policy with "pull" must have the "match_cidr" or "match_username" attribute`,
		},
		{
			RBACPolicyJSON: map[string]any{
				"match_repository":      "library/.+",
				"forbidden_permissions": []string{"pull"},
			},
			ErrorMessage: `RBAC policy with "pull" must have the "match_cidr" or "match_username" attribute`,
		},
		{
			RBACPolicyJSON: map[string]any{
				"match_repository": "library/.+",
				"permissions":      []string{"delete"},
			},
			ErrorMessage: `RBAC policy with "delete" must have the "match_username" attribute`,
		},
		{
			RBACPolicyJSON: map[string]any{
				"match_repository":      "library/.+",
				"forbidden_permissions": []string{"delete"},
			},
			ErrorMessage: `RBAC policy with "delete" must have the "match_username" attribute`,
		},
		{
			RBACPolicyJSON: map[string]any{
				"match_repository": "library/.+",
				"permissions":      []string{"push"},
			},
			ErrorMessage: `RBAC policy with "push" must also grant "pull"`,
		},
		{
			RBACPolicyJSON: map[string]any{
				"match_cidr": "0.0.0.0/64",
			},
			ErrorMessage: `"0.0.0.0/64" is not a valid CIDR`,
		},
		{
			RBACPolicyJSON: map[string]any{
				"match_cidr":       "0.0.0.0/0",
				"match_repository": "test*",
				"permissions":      []string{"pull"},
			},
			ErrorMessage: "0.0.0.0/0 cannot be used as CIDR because it matches everything",
		},
		{
			RBACPolicyJSON: map[string]any{
				"match_repository": "library/.+",
				"match_username":   "foo",
				"permissions":      []string{"anonymous_first_pull"},
			},
			ErrorMessage: `RBAC policy with "anonymous_pull" or "anonymous_first_pull" may not have the "match_username" attribute`,
		},
		{
			RBACPolicyJSON: map[string]any{
				"match_repository":      "library/.+",
				"match_username":        "foo",
				"forbidden_permissions": []string{"anonymous_first_pull"},
			},
			ErrorMessage: `RBAC policy with "anonymous_pull" or "anonymous_first_pull" may not have the "match_username" attribute`,
		},
		{
			RBACPolicyJSON: map[string]any{
				"match_repository": "library/.+",
				"permissions":      []string{"anonymous_first_pull"},
			},
			ErrorMessage: `RBAC policy with "anonymous_first_pull" must also grant "anonymous_pull" or "pull"`,
		},
		{
			RBACPolicyJSON: assert.JSONObject{
				"match_repository": "library/.+",
				"permissions":      []string{"anonymous_first_pull", "anonymous_pull"},
			},
			ErrorMessage: `RBAC policy with "anonymous_first_pull" may only be for external replica accounts`,
		},
		{
			RBACPolicyJSON: map[string]any{
				"match_repository":      "library/.+",
				"forbidden_permissions": []string{"anonymous_first_pull"},
			},
			ErrorMessage: `RBAC policy with "anonymous_first_pull" may only be for external replica accounts`,
		},
		{
			RBACPolicyJSON: map[string]any{
				"match_repository": "*/library",
				"permissions":      []string{"anonymous_pull"},
			},
			ErrorMessage: "request body is not valid JSON: \"*/library\" is not a valid regexp: error parsing regexp: missing argument to repetition operator: `*`",
		},
		{
			RBACPolicyJSON: map[string]any{
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

		h.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
			withPerms("change:tenant1"),
			httptest.WithJSONBody(map[string]any{
				"account": map[string]any{
					"auth_tenant_id": "tenant1",
					"rbac_policies":  []map[string]any{tc.RBACPolicyJSON},
				},
			})).ExpectText(t, expectedStatus, tc.ErrorMessage+"\n")
	}

	// TODO: why is there a positive test in here?
	h.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
				"rbac_policies": []map[string]any{{
					"match_cidr":  "1.2.3.4/16",
					"permissions": []string{"pull"},
				}},
			},
		}),
	).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"account": jsonmatch.Object{
			"auth_tenant_id": "tenant1",
			"metadata":       nil,
			"name":           "first",
			"rbac_policies": []jsonmatch.Object{{
				"match_cidr":  "1.2.0.0/16",
				"permissions": []string{"pull"},
			}},
		},
	})
	tr.DBChanges().AssertEqual(`
		INSERT INTO accounts (name, auth_tenant_id, rbac_policies_json) VALUES ('first', 'tenant1', '[{"match_cidr":"1.2.0.0/16","permissions":["pull"]}]');
	`)
	h.RespondTo(ctx, "GET /keppel/v1/accounts/first", withPerms("view:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"account": jsonmatch.Object{
				"auth_tenant_id": "tenant1",
				"metadata":       nil,
				"name":           "first",
				"rbac_policies": []jsonmatch.Object{{
					"match_cidr":  "1.2.0.0/16",
					"permissions": []string{"pull"},
				}},
			},
		})

	// test unexpected platform filter
	h.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
				"platform_filter": []map[string]any{{
					"os":           "linux",
					"architecture": "amd64",
				}},
			},
		})).ExpectText(t, http.StatusConflict, "cannot change platform filter on existing account\n")

	// test unexpected platform filter on new primary account
	h.RespondTo(ctx, "PUT /keppel/v1/accounts/third",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
				"platform_filter": []map[string]any{{
					"os":           "linux",
					"architecture": "amd64",
				}},
			},
		})).ExpectText(t, http.StatusUnprocessableEntity, "platform filter is only allowed on replica accounts\n")

	// test errors for sublease token issuance: missing authentication/authorization
	resp = h.RespondTo(ctx, "POST /keppel/v1/accounts/first/sublease")
	assert.Equal(t, resp.Header().Get("Www-Authenticate"),
		// default auth is bearer token auth, so an auth challenge gets rendered
		`Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="keppel_account:first:change"`,
	)
	resp.ExpectText(t, http.StatusForbidden, "no bearer token found in request headers\n")

	h.RespondTo(ctx, "POST /keppel/v1/accounts/first/sublease",
		withPerms("view:tenant1"),
	).ExpectText(t, http.StatusForbidden, "no permission for keppel_account:first:change\n")
	h.RespondTo(ctx, "POST /keppel/v1/accounts/unknown/sublease", // account does not exist
		withPerms("view:tenant1,change:tenant1"),
	).ExpectText(t, http.StatusForbidden, "no permission for keppel_account:unknown:change\n")

	h.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
				"in_maintenance": true, // this field used to be supported, but support for it was removed
			},
		})).ExpectText(t, http.StatusBadRequest, "request body is not valid JSON: json: unknown field \"in_maintenance\"\n")

	h.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
				"metadata":       map[string]string{"foo": "bar"},
			},
		})).ExpectText(t, http.StatusUnprocessableEntity, "malformed attribute \"account.metadata\" in request body does no longer exist\n")

	h.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
				"name":           "first", // setting the name to its existing value is pointless, but allowed
			},
		})).ExpectStatus(t, http.StatusOK)

	h.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
				"name":           "second",
			},
		})).ExpectText(t, http.StatusUnprocessableEntity, "changing attribute \"account.name\" in request body is not allowed\n")

	// test protection for managed accounts
	test.MustExec(t, s.DB, "UPDATE accounts SET is_managed = TRUE WHERE name = $1", "first")
	h.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"account": map[string]any{
				"auth_tenant_id": "tenant1",
			},
		})).ExpectText(t, http.StatusForbidden, "cannot manually change configuration of a managed account\n")
	test.MustExec(t, s.DB, "UPDATE accounts SET is_managed = FALSE WHERE name = $1", "first")
}

func TestGetPutAccountReplicationOnFirstUse(t *testing.T) {
	ctx := t.Context()
	test.WithRoundTripper(func(tt *test.RoundTripper) {
		s1 := test.NewSetup(t, test.WithKeppelAPI, test.WithPeerAPI)
		s2 := test.NewSetup(t, test.WithKeppelAPI, test.IsSecondaryTo(&s1))

		s1.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
			withPerms("change:tenant1"),
			httptest.WithJSONBody(map[string]any{
				"account": map[string]any{
					"auth_tenant_id": "tenant1",
				},
			}),
		).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"account": jsonmatch.Object{
				"name":           "first",
				"auth_tenant_id": "tenant1",
				"metadata":       nil,
				"rbac_policies":  []jsonmatch.Object{},
			},
		})

		// test error cases on creation
		s2.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
			withPerms("change:tenant1"),
			httptest.WithJSONBody(map[string]any{
				"account": map[string]any{
					"auth_tenant_id": "tenant1",
					"replication":    map[string]any{"strategy": "yes_please", "upstream": "registry.example.org"},
				},
			})).ExpectText(t, http.StatusBadRequest, "request body is not valid JSON: do not know how to deserialize ReplicationPolicy with strategy \"yes_please\"\n")

		s2.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
			withPerms("change:tenant1"),
			httptest.WithJSONBody(map[string]any{
				"account": map[string]any{
					"auth_tenant_id": "tenant1",
					"replication":    map[string]any{"strategy": "on_first_use", "upstream": "someone-else.example.org"},
				},
			})).ExpectText(t, http.StatusUnprocessableEntity, "unknown peer registry: \"someone-else.example.org\"\n")

		s2.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
			withPerms("change:tenant1"),
			httptest.WithJSONBody(map[string]any{
				"account": map[string]any{
					"auth_tenant_id": "tenant1",
					"replication":    map[string]any{"strategy": "on_first_use", "upstream": "registry.example.org"},
				},
			})).ExpectText(t, http.StatusForbidden, "wrong sublease token\n")

		s2.FD.ValidSubleaseTokenSecrets["first"] = "valid-token"
		s2.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
			withPerms("change:tenant1"),
			httptest.WithHeader(keppelv1.SubleaseHeader, makeSubleaseToken("first", "registry.example.org", "not-the-valid-token")),
			httptest.WithJSONBody(map[string]any{
				"account": map[string]any{
					"auth_tenant_id": "tenant1",
					"replication":    map[string]any{"strategy": "on_first_use", "upstream": "registry.example.org"},
				},
			})).ExpectText(t, http.StatusForbidden, "wrong sublease token\n")

		// test PUT success case
		s2.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
			withPerms("change:tenant1"),
			httptest.WithHeader(keppelv1.SubleaseHeader, makeSubleaseToken("first", "registry.example.org", "valid-token")),
			httptest.WithJSONBody(map[string]any{
				"account": map[string]any{
					"auth_tenant_id": "tenant1",
					"replication":    map[string]any{"strategy": "on_first_use", "upstream": "registry.example.org"},
				},
			}),
		).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"account": jsonmatch.Object{
				"name":           "first",
				"auth_tenant_id": "tenant1",
				"metadata":       nil,
				"rbac_policies":  []jsonmatch.Object{},
				"replication": jsonmatch.Object{
					"strategy": "on_first_use",
					"upstream": "registry.example.org",
				},
			},
		})

		// PUT on existing account with replication unspecified is okay, leaves
		// replication settings unchanged
		s2.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
			withPerms("change:tenant1"),
			httptest.WithJSONBody(map[string]any{
				"account": map[string]any{
					"auth_tenant_id": "tenant1",
				},
			}),
		).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"account": jsonmatch.Object{
				"name":           "first",
				"auth_tenant_id": "tenant1",
				"metadata":       nil,
				"rbac_policies":  []jsonmatch.Object{},
				"replication": jsonmatch.Object{
					"strategy": "on_first_use",
					"upstream": "registry.example.org",
				},
			},
		})

		// cannot issue sublease token for replica account (only for primary accounts)
		s2.Handler.RespondTo(ctx, "POST /keppel/v1/accounts/first/sublease", withPerms("view:tenant1,change:tenant1")).
			ExpectText(t, http.StatusBadRequest, "operation not allowed for replica accounts\n")

		// PUT on existing account with different replication settings is not allowed
		s2.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/second",
			withPerms("change:tenant2"),
			httptest.WithJSONBody(map[string]any{
				"account": map[string]any{
					"auth_tenant_id": "tenant2",
				},
			}),
		).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"account": jsonmatch.Object{
				"name":           "second",
				"auth_tenant_id": "tenant2",
				"metadata":       nil,
				"rbac_policies":  []jsonmatch.Object{},
			},
		})
		s2.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/second",
			withPerms("change:tenant2"),
			httptest.WithJSONBody(map[string]any{
				"account": map[string]any{
					"auth_tenant_id": "tenant2",
					"replication":    map[string]any{"strategy": "on_first_use", "upstream": "registry.example.org"},
				},
			})).ExpectText(t, http.StatusConflict, "cannot change replication policy on existing account\n")
	})
}

func TestGetPutAccountReplicationFromExternalOnFirstUse(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler
	ctx := t.Context()

	// helper functions for basic PUT calls on accounts
	putFirstAccount := func(accountConfig map[string]any) httptest.Response {
		return h.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
			withPerms("change:tenant1"),
			httptest.WithJSONBody(map[string]any{"account": accountConfig}),
		)
	}
	putSecondAccount := func(accountConfig map[string]any) httptest.Response {
		return h.RespondTo(ctx, "PUT /keppel/v1/accounts/second",
			withPerms("change:tenant2"),
			httptest.WithJSONBody(map[string]any{"account": accountConfig}),
		)
	}

	// test error cases on creation
	putFirstAccount(map[string]any{
		"auth_tenant_id": "tenant1",
		"replication": map[string]any{
			"strategy": "from_external_on_first_use",
			"upstream": "registry.example.org",
		},
	}).ExpectText(t, http.StatusBadRequest, "request body is not valid JSON: json: cannot unmarshal string into Go struct field Account.account.replication of type keppel.ReplicationExternalPeerSpec\n")

	putFirstAccount(map[string]any{
		"auth_tenant_id": "tenant1",
		"replication": map[string]any{
			"strategy": "from_external_on_first_use",
			"upstream": map[string]any{
				"not": "what-you-expect",
			},
		},
	}).ExpectText(t, http.StatusUnprocessableEntity, "missing upstream URL for \"from_external_on_first_use\" replication\n")

	putFirstAccount(map[string]any{
		"auth_tenant_id": "tenant1",
		"replication": map[string]any{
			"strategy": "from_external_on_first_use",
			"upstream": map[string]any{
				"url":      "registry.example.com",
				"username": "keks",
			},
		},
	}).ExpectText(t, http.StatusUnprocessableEntity, "need either both username and password or neither for \"from_external_on_first_use\" replication\n")

	putFirstAccount(map[string]any{
		"auth_tenant_id": "tenant1",
		"replication": map[string]any{
			"strategy": "from_external_on_first_use",
			"upstream": map[string]any{
				"url":      "registry.example.com",
				"password": "keks",
			},
		},
	}).ExpectText(t, http.StatusUnprocessableEntity, "need either both username and password or neither for \"from_external_on_first_use\" replication\n")

	// test PUT success case
	testPlatformFilter := []map[string]any{
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
	putFirstAccount(map[string]any{
		"auth_tenant_id": "tenant1",
		"replication": map[string]any{
			"strategy": "from_external_on_first_use",
			"upstream": map[string]any{
				"url": "registry.example.com",
			},
		},
		"platform_filter": testPlatformFilter,
	}).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"account": jsonmatch.Object{
			"name":           "first",
			"auth_tenant_id": "tenant1",
			"metadata":       nil,
			"rbac_policies":  []jsonmatch.Object{},
			"replication": jsonmatch.Object{
				"strategy": "from_external_on_first_use",
				"upstream": jsonmatch.Object{
					"url": "registry.example.com",
				},
			},
			"platform_filter": testPlatformFilter,
		},
	})

	// PUT on existing account with replication unspecified is okay, leaves
	// replication settings unchanged
	putFirstAccount(map[string]any{
		"auth_tenant_id": "tenant1",
	}).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"account": jsonmatch.Object{
			"name":           "first",
			"auth_tenant_id": "tenant1",
			"metadata":       nil,
			"rbac_policies":  []jsonmatch.Object{},
			"replication": jsonmatch.Object{
				"strategy": "from_external_on_first_use",
				"upstream": jsonmatch.Object{
					"url": "registry.example.com",
				},
			},
			"platform_filter": testPlatformFilter,
		},
	})

	// test PUT on existing account to update replication credentials
	putFirstAccount(map[string]any{
		"auth_tenant_id": "tenant1",
		"replication": map[string]any{
			"strategy": "from_external_on_first_use",
			"upstream": map[string]any{
				"url":      "registry.example.com",
				"username": "foo",
				"password": "bar",
			},
		},
	}).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"account": jsonmatch.Object{
			"name":           "first",
			"auth_tenant_id": "tenant1",
			"metadata":       nil,
			"rbac_policies":  []jsonmatch.Object{},
			"replication": jsonmatch.Object{
				"strategy": "from_external_on_first_use",
				"upstream": jsonmatch.Object{
					"url":      "registry.example.com",
					"username": "foo",
				},
			},
			"platform_filter": testPlatformFilter,
		},
	})

	// PUT on existing account with replication credentials section copied from
	// GET is okay, leaves replication settings unchanged too (this is important
	// because, in practice, clients copy the account config from GET, change a
	// thing, and PUT the result)
	putFirstAccount(map[string]any{
		"auth_tenant_id": "tenant1",
		"rbac_policies":  []map[string]any{},
		"replication": map[string]any{
			"strategy": "from_external_on_first_use",
			"upstream": map[string]any{
				"url":      "registry.example.com",
				"username": "foo",
			},
		},
		"platform_filter": testPlatformFilter,
	}).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"account": jsonmatch.Object{
			"name":           "first",
			"auth_tenant_id": "tenant1",
			"metadata":       nil,
			"rbac_policies":  []jsonmatch.Object{},
			"replication": jsonmatch.Object{
				"strategy": "from_external_on_first_use",
				"upstream": jsonmatch.Object{
					"url":      "registry.example.com",
					"username": "foo",
				},
			},
			"platform_filter": testPlatformFilter,
		},
	})

	// ...but changing the username without also supplying a password is wrong
	putFirstAccount(map[string]any{
		"auth_tenant_id": "tenant1",
		"rbac_policies":  []map[string]any{},
		"replication": map[string]any{
			"strategy": "from_external_on_first_use",
			"upstream": map[string]any{
				"url":      "registry.example.com",
				"username": "bar",
			},
		},
		"platform_filter": testPlatformFilter,
	}).ExpectText(t, http.StatusUnprocessableEntity, "cannot change username for \"from_external_on_first_use\" replication without also changing password\n")

	// test sublease token issuance on account (external replicas count as primary
	// accounts for the purposes of account name subleasing)
	s.FD.NextSubleaseTokenSecretToIssue = "this-is-the-token"
	expectedToken := makeSubleaseToken("first", "registry.example.org", "this-is-the-token")
	h.RespondTo(ctx, "POST /keppel/v1/accounts/first/sublease", withPerms("view:tenant1,change:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{"sublease_token": expectedToken})

	// PUT on existing account with different replication settings is not allowed
	putFirstAccount(map[string]any{
		"auth_tenant_id": "tenant1",
		"replication": map[string]any{
			"strategy": "from_external_on_first_use",
			"upstream": map[string]any{
				"url":      "other-registry.example.com",
				"username": "foo",
				"password": "bar",
			},
		},
	}).ExpectText(t, http.StatusConflict, "cannot change replication policy on existing account\n")

	putSecondAccount(map[string]any{
		"auth_tenant_id": "tenant2",
	}).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"account": jsonmatch.Object{
			"name":           "second",
			"auth_tenant_id": "tenant2",
			"metadata":       nil,
			"rbac_policies":  []map[string]any{},
		},
	})

	putSecondAccount(map[string]any{
		"auth_tenant_id": "tenant2",
		"replication": map[string]any{
			"strategy": "from_external_on_first_use",
			"upstream": map[string]any{
				"url":      "other-registry.example.com",
				"username": "foo",
				"password": "bar",
			},
		},
	}).ExpectText(t, http.StatusConflict, "cannot change replication policy on existing account\n")

	// PUT on existing account with different platform filter is not allowed
	putFirstAccount(map[string]any{
		"auth_tenant_id": "tenant1",
		"replication": map[string]any{
			"strategy": "from_external_on_first_use",
			"upstream": map[string]any{
				"url":      "registry.example.com",
				"username": "foo",
				"password": "bar",
			},
		},
		"platform_filter": []map[string]any{},
	}).ExpectText(t, http.StatusConflict, "cannot change platform filter on existing account\n")
}

func TestDeleteAccount(t *testing.T) {
	s := test.NewSetup(t,
		test.WithKeppelAPI,
		test.WithAccount(models.Account{Name: "test1", AuthTenantID: "tenant1"}),
	)
	h := s.Handler
	ctx := t.Context()

	tr, tr0 := easypg.NewTracker(t, s.DB.Db)
	tr0.Ignore()

	// failure case: insufficient permissions (the "delete" permission refers to
	// manifests within the account, not the account itself)
	h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test1", withPerms("view:tenant1,delete:tenant1")).
		ExpectStatus(t, http.StatusForbidden)

	// DELETE on account should immediately mark it for deletion
	h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test1", withPerms("view:tenant1,change:tenant1")).
		ExpectStatus(t, http.StatusNoContent)

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
	h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test1", withPerms("view:tenant1,change:tenant1")).
		ExpectStatus(t, http.StatusNoContent)

	tr.DBChanges().AssertEmpty()
	s.Auditor.ExpectEvents(t /*, nothing */)
}

//nolint:unparam
func makeSubleaseToken(accountName, primaryHostname, secret string) string {
	buf, _ := json.Marshal(map[string]any{
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
	ctx := t.Context()
	test.WithRoundTripper(func(tt *test.RoundTripper) {
		s1 := test.NewSetup(t, test.WithKeppelAPI, test.WithPeerAPI)
		s2 := test.NewSetup(t, test.WithKeppelAPI, test.IsSecondaryTo(&s1))

		testPlatformFilter := []map[string]any{
			{
				"os":           "linux",
				"architecture": "amd64",
			},
		}

		// create some primary accounts to play with
		for _, name := range []models.AccountName{"first", "second", "third"} {
			s1.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/"+string(name),
				withPerms("change:tenant1"),
				httptest.WithJSONBody(map[string]any{
					"account": map[string]any{
						"auth_tenant_id": "tenant1",
						"replication": map[string]any{
							"strategy": "from_external_on_first_use",
							"upstream": map[string]any{
								"url": "registry.example.org",
							},
						},
						"platform_filter": testPlatformFilter,
					},
				}),
			).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
				"account": jsonmatch.Object{
					"name":           name,
					"auth_tenant_id": "tenant1",
					"metadata":       nil,
					"rbac_policies":  []jsonmatch.Object{},
					"replication": jsonmatch.Object{
						"strategy": "from_external_on_first_use",
						"upstream": jsonmatch.Object{
							"url": "registry.example.org",
						},
					},
					"platform_filter": testPlatformFilter,
				},
			})
			s2.FD.ValidSubleaseTokenSecrets[name] = "valid-token"
		}

		// create an account which inherits the PlatformFilter
		s2.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/first",
			withPerms("change:tenant1"),
			httptest.WithHeader(keppelv1.SubleaseHeader, makeSubleaseToken("first", "registry.example.org", "valid-token")),
			httptest.WithJSONBody(map[string]any{
				"account": map[string]any{
					"auth_tenant_id": "tenant1",
					"replication": map[string]any{
						"strategy": "on_first_use",
						"upstream": "registry.example.org",
					},
				},
			}),
		).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"account": jsonmatch.Object{
				"name":            "first",
				"auth_tenant_id":  "tenant1",
				"metadata":        nil,
				"platform_filter": testPlatformFilter,
				"rbac_policies":   []jsonmatch.Object{},
				"replication": jsonmatch.Object{
					"strategy": "on_first_use",
					"upstream": "registry.example.org",
				},
			},
		})

		// create an account with the same PlatformFilter
		s2.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/second",
			withPerms("change:tenant1"),
			httptest.WithHeader(keppelv1.SubleaseHeader, makeSubleaseToken("second", "registry.example.org", "valid-token")),
			httptest.WithJSONBody(map[string]any{
				"account": map[string]any{
					"auth_tenant_id": "tenant1",
					"metadata":       nil,
					"platform_filter": []map[string]any{{
						"os":           "linux",
						"architecture": "amd64",
					}},
					"replication": map[string]any{
						"strategy": "on_first_use",
						"upstream": "registry.example.org",
					},
				},
			}),
		).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"account": jsonmatch.Object{
				"name":            "second",
				"auth_tenant_id":  "tenant1",
				"metadata":        nil,
				"platform_filter": testPlatformFilter,
				"rbac_policies":   []jsonmatch.Object{},
				"replication": jsonmatch.Object{
					"strategy": "on_first_use",
					"upstream": "registry.example.org",
				},
			},
		})

		// create an account with an incompatible PlatformFilter
		s2.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/third",
			withPerms("change:tenant1"),
			httptest.WithHeader(keppelv1.SubleaseHeader, makeSubleaseToken("third", "registry.example.org", "valid-token")),
			httptest.WithJSONBody(map[string]any{
				"account": map[string]any{
					"auth_tenant_id": "tenant1",
					"platform_filter": []map[string]any{{
						"os":           "linux",
						"architecture": "arm64",
						"variant":      "v8",
					}},
					"replication": map[string]any{
						"strategy": "on_first_use",
						"upstream": "registry.example.org",
					},
				},
			}),
		).ExpectText(t, http.StatusConflict,
			"peer account filter needs to match primary account filter: local account [{\"architecture\":\"arm64\",\"os\":\"linux\",\"variant\":\"v8\"}], peer account [{\"architecture\":\"amd64\",\"os\":\"linux\"}] \n",
		)
	})
}

func TestSecurityScanPoliciesHappyPath(t *testing.T) {
	s := test.NewSetup(t,
		test.WithKeppelAPI,
		test.WithAccount(models.Account{Name: "first", AuthTenantID: "tenant1"}),
	)
	ctx := t.Context()

	// we need to set test.AuthDriver.ExpectedUserName because this username is
	// matched against the managed_by_user field of our policies
	s.AD.ExpectedUserName = "exampleuser"

	// a freshly-created account should have no policies at all
	s.Handler.RespondTo(ctx, "GET /keppel/v1/accounts/first/security_scan_policies", withPerms("view:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{"policies": []jsonmatch.Object{}})
	s.Auditor.IgnoreEventsUntilNow()

	// helper function for testing a successful PUT of policies, followed by a GET
	// that returns those same policies
	expectPoliciesToBeApplied := func(policies ...map[string]any) {
		s.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/first/security_scan_policies",
			withPerms("change:tenant1"),
			httptest.WithJSONBody(map[string]any{"policies": policies}),
		).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"policies": policies,
		})

		s.Handler.RespondTo(ctx, "GET /keppel/v1/accounts/first/security_scan_policies",
			withPerms("view:tenant1"),
		).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"policies": policies,
		})
	}

	// PUT with no policies is okay, does nothing
	expectPoliciesToBeApplied( /* nothing */ )
	s.Auditor.ExpectEvents(t /*, nothing */)

	// add the policies from the API spec example
	policy1 := map[string]any{
		"match_repository":       ".*",
		"match_vulnerability_id": ".*",
		"except_fix_released":    true,
		"action": map[string]any{
			"ignore":     true,
			"assessment": "risk accepted: vulnerabilities without an available fix are not actionable",
		},
	}
	policy2 := map[string]any{
		"managed_by_user":        "exampleuser",
		"match_repository":       "my-python-app|my-other-image",
		"match_vulnerability_id": "CVE-2022-40897",
		"action": map[string]any{
			"severity":   "Low",
			"assessment": "adjusted severity: python-setuptools cannot be invoked through user requests",
		},
	}
	expectPoliciesToBeApplied(policy1, policy2)

	// adding two policies generates one create event per policy
	expectedEventForPolicy := func(action cadf.Action, policy map[string]any) cadf.Event {
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
	s.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/first/security_scan_policies",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"policies": []map[string]any{{
				"managed_by_user":        "$REQUESTER",
				"match_repository":       ".*",
				"match_vulnerability_id": ".*",
				"except_fix_released":    true,
				"action": map[string]any{
					"ignore":     true,
					"assessment": "risk accepted: vulnerabilities without an available fix are not actionable",
				},
			}},
		}),
	).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"policies": []jsonmatch.Object{{
			"managed_by_user":        "exampleuser",
			"match_repository":       ".*",
			"match_vulnerability_id": ".*",
			"except_fix_released":    true,
			"action": jsonmatch.Object{
				"ignore":     true,
				"assessment": "risk accepted: vulnerabilities without an available fix are not actionable",
			},
		}},
	})
	s.Auditor.IgnoreEventsUntilNow()
}

func TestSecurityScanPoliciesValidationErrors(t *testing.T) {
	s := test.NewSetup(t,
		test.WithKeppelAPI,
		test.WithAccount(models.Account{Name: "first", AuthTenantID: "tenant1"}),
	)
	ctx := t.Context()

	// we need to set test.AuthDriver.ExpectedUserName because this username is
	// matched against the managed_by_user field of our policies
	s.AD.ExpectedUserName = "exampleuser"

	// helper to set security scan policies
	setPolicies := func(policies []map[string]any) httptest.Response {
		return s.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/first/security_scan_policies",
			withPerms("change:tenant1"),
			httptest.WithJSONBody(map[string]any{"policies": policies}),
		)
	}

	// check unmarshalling errors
	setPolicies([]map[string]any{
		{
			"match_repository":       ".*",
			"match_vulnerability_id": ".*",
			"action": map[string]any{
				"severity":   "Low",
				"assessment": "not important",
			},
			"unknown_field": 42,
		},
	}).ExpectText(t, http.StatusBadRequest, "request body is not valid JSON: json: unknown field \"unknown_field\"\n")

	setPolicies([]map[string]any{
		{
			"match_repository":       ".*",
			"match_vulnerability_id": "(.*",
			"action": map[string]any{
				"severity":   "Low",
				"assessment": "not important",
			},
		},
	}).ExpectText(t, http.StatusBadRequest, "request body is not valid JSON: \"(.*\" is not a valid regexp: error parsing regexp: missing closing ): `^(?:(.*)$`\n")

	// check all policy-local validations (every policy has exactly one error)
	setPolicies([]map[string]any{
		{
			// missing "match_repository"
			"match_vulnerability_id": ".*",
			"action": map[string]any{
				"severity":   "Low",
				"assessment": "not important",
			},
		},
		{
			// missing "match_vulnerability"
			"match_repository": ".*",
			"action": map[string]any{
				"severity":   "Low",
				"assessment": "not important",
			},
		},
		{
			// missing "assessment"
			"match_repository":       ".*",
			"match_vulnerability_id": ".*",
			"action": map[string]any{
				"severity": "Low",
			},
		},
		{
			// overlong "assessment"
			"match_repository":       ".*",
			"match_vulnerability_id": ".*",
			"action": map[string]any{
				"severity":   "Low",
				"assessment": strings.Repeat("a", 1025),
			},
		},
		{
			// both "severity" and "ignore"
			"match_repository":       ".*",
			"match_vulnerability_id": ".*",
			"action": map[string]any{
				"severity":   "Clean",
				"ignore":     true,
				"assessment": "not important",
			},
		},
		{
			// neither "severity" nor "ignore"
			"match_repository":       ".*",
			"match_vulnerability_id": ".*",
			"action": map[string]any{
				"assessment": "not important",
			},
		},
		{
			// unknown value for "severity"
			"match_repository":       ".*",
			"match_vulnerability_id": ".*",
			"action": map[string]any{
				"severity":   "Pending",
				"assessment": "not important",
			},
		},
		{
			// unacceptable value for "severity" (must be an explicit value)
			"match_repository":       ".*",
			"match_vulnerability_id": ".*",
			"action": map[string]any{
				"severity":   "Unknown",
				"assessment": "not important",
			},
		},
	}).ExpectText(t, http.StatusUnprocessableEntity, strings.Join([]string{
		`policies[0] must have the "match_repository" attribute`,
		`policies[1] must have the "match_vulnerability_id" attribute`,
		`policies[2].action must have the "assessment" attribute`,
		`policies[3].action.assessment cannot be larger than 1 KiB`,
		`policies[4].action cannot have the "severity" attribute when "ignore" is set`,
		`policies[5].action must have the "severity" attribute when "ignore" is not set`,
		`policies[6].action.severity contains the invalid value "Pending"`,
		`policies[7].action.severity contains the invalid value "Unknown"`,
	}, "\n")+"\n")
}

func TestSecurityScanPoliciesAuthorizationErrors(t *testing.T) {
	s := test.NewSetup(t,
		test.WithKeppelAPI,
		test.WithAccount(models.Account{Name: "first", AuthTenantID: "tenant1"}),
	)
	ctx := t.Context()

	// we need to set test.AuthDriver.ExpectedUserName because this username is
	// matched against the managed_by_user field of our policies
	s.AD.ExpectedUserName = "exampleuser"

	// PUT requires CanChangeAccount
	s.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/first/security_scan_policies",
		withPerms("view:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"policies": []map[string]any{},
		}),
	).ExpectStatus(t, http.StatusForbidden)

	// we should not be allowed to put in policies under a different user's name
	foreignPolicy := map[string]any{
		"managed_by_user":        "johndoe",
		"match_repository":       ".*",
		"match_vulnerability_id": ".*",
		"action": map[string]any{
			"assessment": "not important",
			"severity":   "Low",
		},
	}
	foreignPolicyJSON := toJSONVia[keppel.SecurityScanPolicy](foreignPolicy)
	s.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/first/security_scan_policies",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"policies": []map[string]any{foreignPolicy},
		}),
	).ExpectText(t, http.StatusUnprocessableEntity,
		fmt.Sprintf("cannot apply this new or updated policy that is managed by a different user: %s\n", foreignPolicyJSON),
	)

	// as preparation for the next test, put in a pre-existing policy managed by a
	// different user
	test.MustExec(t, s.DB, `UPDATE accounts SET security_scan_policies_json = $1`,
		fmt.Sprintf("[%s]", foreignPolicyJSON))

	// it's okay if we leave that policy untouched...
	s.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/first/security_scan_policies",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"policies": []map[string]any{foreignPolicy},
		}),
	).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"policies": []jsonmatch.Object{foreignPolicy},
	})

	// ...but updating is not okay...
	delete(foreignPolicy, "managed_by_user")
	foreignPolicy["match_repository"] = "definitely-not-the-old-value"
	s.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/first/security_scan_policies",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"policies": []map[string]any{foreignPolicy},
		}),
	).ExpectText(t, http.StatusUnprocessableEntity,
		fmt.Sprintf("cannot update or delete this existing policy that is managed by a different user: %s\n", foreignPolicyJSON),
	)

	// ...and deleting is also not okay
	s.Handler.RespondTo(ctx, "PUT /keppel/v1/accounts/first/security_scan_policies",
		withPerms("change:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"policies": []map[string]any{},
		}),
	).ExpectText(t, http.StatusUnprocessableEntity,
		fmt.Sprintf("cannot update or delete this existing policy that is managed by a different user: %s\n", foreignPolicyJSON),
	)
}
