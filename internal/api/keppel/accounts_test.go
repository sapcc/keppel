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
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/hermes/pkg/cadf"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

////////////////////////////////////////////////////////////////////////////////
// recorder for audit events

type testAuditor struct {
	Events []cadf.Event
}

func (a *testAuditor) Record(params audittools.EventParameters) {
	a.Events = append(a.Events, a.Normalize(audittools.NewEvent(params)))
}

func (a *testAuditor) Reset() {
	//reset state for next test
	a.Events = nil
}

func (a *testAuditor) ExpectEvents(t *testing.T, expectedEvents ...cadf.Event) {
	t.Helper()
	if len(expectedEvents) == 0 {
		expectedEvents = nil
	} else {
		for idx, event := range expectedEvents {
			expectedEvents[idx] = a.Normalize(event)
		}
	}
	assert.DeepEqual(t, "CADF events", a.Events, expectedEvents)
	a.Reset()
}

func (a *testAuditor) Normalize(event cadf.Event) cadf.Event {
	//overwrite some attributes where we don't care about variance
	event.TypeURI = "http://schemas.dmtf.org/cloud/audit/1.0/event"
	event.ID = "00000000-0000-0000-0000-000000000000"
	event.EventTime = "2006-01-02T15:04:05.999999+00:00"
	event.EventType = "activity"
	event.Initiator = cadf.Resource{}
	event.Observer = cadf.Resource{}
	return event
}

////////////////////////////////////////////////////////////////////////////////
// some helpers to make the cadf.Event literals shorter

var (
	cadfReasonOK = cadf.Reason{
		ReasonType: "HTTP",
		ReasonCode: "200",
	}
)

func toJSON(x interface{}) string {
	result, _ := json.Marshal(x)
	return string(result)
}

////////////////////////////////////////////////////////////////////////////////
// tests

func setup(t *testing.T) (http.Handler, *test.AuthDriver, *test.NameClaimDriver, *testAuditor, keppel.StorageDriver, *keppel.DB) {
	cfg, db := test.Setup(t)

	ad, err := keppel.NewAuthDriver("unittest")
	if err != nil {
		t.Fatal(err.Error())
	}
	ncd, err := keppel.NewNameClaimDriver("unittest", ad, cfg)
	if err != nil {
		t.Fatal(err.Error())
	}
	sd, err := keppel.NewStorageDriver("in-memory-for-testing", ad, cfg)
	if err != nil {
		t.Fatal(err.Error())
	}

	r := mux.NewRouter()
	auditor := &testAuditor{}
	NewAPI(cfg, ad, ncd, sd, db, auditor).AddTo(r)

	return r, ad.(*test.AuthDriver), ncd.(*test.NameClaimDriver), auditor, sd, db
}

func TestAccountsAPI(t *testing.T) {
	r, authDriver, _, auditor, _, _ := setup(t)

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

		//only the first pass should generate an audit event
		if pass == 1 {
			auditor.ExpectEvents(t, cadf.Event{
				RequestPath: "/keppel/v1/accounts/first",
				Action:      "create",
				Outcome:     "success",
				Reason:      cadfReasonOK,
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

		//only the first pass should generate audit events
		if pass == 1 {
			auditor.ExpectEvents(t,
				cadf.Event{
					RequestPath: "/keppel/v1/accounts/second",
					Action:      "create",
					Outcome:     "success",
					Reason:      cadfReasonOK,
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
					Reason:      cadfReasonOK,
					Target: cadf.Resource{
						TypeURI:   "docker-registry/account",
						ID:        "second",
						ProjectID: "tenant1",
						Attachments: []cadf.Attachment{{
							Name:    "payload",
							TypeURI: "mime:application/json",
							Content: toJSON(rbacPoliciesJSON[0]),
						}},
					},
				},
				cadf.Event{
					RequestPath: "/keppel/v1/accounts/second",
					Action:      "create/rbac-policy",
					Outcome:     "success",
					Reason:      cadfReasonOK,
					Target: cadf.Resource{
						TypeURI:   "docker-registry/account",
						ID:        "second",
						ProjectID: "tenant1",
						Attachments: []cadf.Attachment{{
							Name:    "payload",
							TypeURI: "mime:application/json",
							Content: toJSON(rbacPoliciesJSON[1]),
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
				"rbac_policies":  newRBACPoliciesJSON,
			},
		},
	}.Check(t, r)
	auditor.ExpectEvents(t,
		cadf.Event{
			RequestPath: "/keppel/v1/accounts/second",
			Action:      "update/rbac-policy",
			Outcome:     "success",
			Reason:      cadfReasonOK,
			Target: cadf.Resource{
				TypeURI:   "docker-registry/account",
				ID:        "second",
				ProjectID: "tenant1",
				Attachments: []cadf.Attachment{{
					Name:    "payload",
					TypeURI: "mime:application/json",
					Content: toJSON(newRBACPoliciesJSON[0]),
				}, {
					Name:    "payload-before",
					TypeURI: "mime:application/json",
					Content: toJSON(rbacPoliciesJSON[1]),
				}},
			},
		},
		cadf.Event{
			RequestPath: "/keppel/v1/accounts/second",
			Action:      "create/rbac-policy",
			Outcome:     "success",
			Reason:      cadfReasonOK,
			Target: cadf.Resource{
				TypeURI:   "docker-registry/account",
				ID:        "second",
				ProjectID: "tenant1",
				Attachments: []cadf.Attachment{{
					Name:    "payload",
					TypeURI: "mime:application/json",
					Content: toJSON(newRBACPoliciesJSON[1]),
				}},
			},
		},
		cadf.Event{
			RequestPath: "/keppel/v1/accounts/second",
			Action:      "delete/rbac-policy",
			Outcome:     "success",
			Reason:      cadfReasonOK,
			Target: cadf.Resource{
				TypeURI:   "docker-registry/account",
				ID:        "second",
				ProjectID: "tenant1",
				Attachments: []cadf.Attachment{{
					Name:    "payload",
					TypeURI: "mime:application/json",
					Content: toJSON(rbacPoliciesJSON[0]),
				}},
			},
		},
	)
}

func TestGetAccountsErrorCases(t *testing.T) {
	r, _, _, _, _, _ := setup(t)

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
	r, _, ncd, _, _, _ := setup(t)

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
}

func TestGetPutAccountReplicationOnFirstUse(t *testing.T) {
	r, _, _, _, _, db := setup(t)

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
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("unknown replication strategy: \"yes_please\"\n"),
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

	//test PUT success case
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
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"account": assert.JSONObject{
				"name":           "first",
				"auth_tenant_id": "tenant1",
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
				"rbac_policies":  []assert.JSONObject{},
				"replication": assert.JSONObject{
					"strategy": "on_first_use",
					"upstream": "peer.example.org",
				},
			},
		},
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
