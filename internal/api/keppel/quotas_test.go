/******************************************************************************
*
*  Copyright 2019 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package keppelv1

import (
	"net/http"
	"testing"
	"time"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/hermes/pkg/cadf"
	"github.com/sapcc/keppel/internal/clair"
	"github.com/sapcc/keppel/internal/keppel"
)

func TestQuotasAPI(t *testing.T) {
	r, _, _, auditor, _, db := setup(t)

	//GET on auth tenant without more specific configuration shows default values
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/quotas/tenant1",
		Header:       map[string]string{"X-Test-Perms": "viewquota:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"manifests": assert.JSONObject{"quota": 0, "usage": 0},
		},
	}.Check(t, r)

	//GET basic error cases
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/quotas/tenant1",
		Header:       map[string]string{"X-Test-Perms": "viewquota:tenant2"},
		ExpectStatus: http.StatusForbidden,
	}.Check(t, r)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/quotas/tenant1",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusForbidden,
	}.Check(t, r)

	//PUT happy case
	for _, pass := range []int{1, 2, 3} {
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/quotas/tenant1",
			Header: map[string]string{"X-Test-Perms": "changequota:tenant1"},
			Body: assert.JSONObject{
				"manifests": assert.JSONObject{"quota": 100},
			},
			ExpectStatus: http.StatusOK,
			ExpectBody: assert.JSONObject{
				"manifests": assert.JSONObject{"quota": 100, "usage": 0},
			},
		}.Check(t, r)

		//only the first pass should generate an audit event
		if pass == 1 {
			auditor.ExpectEvents(t, cadf.Event{
				RequestPath: "/keppel/v1/quotas/tenant1",
				Action:      "update",
				Outcome:     "success",
				Reason:      cadfReasonOK,
				Target: cadf.Resource{
					TypeURI:   "docker-registry/project-quota",
					ID:        "tenant1",
					ProjectID: "tenant1",
					Attachments: []cadf.Attachment{
						{
							Name:    "payload-before",
							TypeURI: "mime:application/json",
							Content: `{"manifests":0}`,
						},
						{
							Name:    "payload",
							TypeURI: "mime:application/json",
							Content: `{"manifests":100}`,
						},
					},
				},
			})
		} else {
			auditor.ExpectEvents(t /*, nothing */)
		}
	}

	//GET reflects changes
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/quotas/tenant1",
		Header:       map[string]string{"X-Test-Perms": "viewquota:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"manifests": assert.JSONObject{"quota": 100, "usage": 0},
		},
	}.Check(t, r)

	//put some manifests in the DB, check thet GET reflects higher usage
	mustInsert(t, db, &keppel.Account{
		Name:         "test1",
		AuthTenantID: "tenant1",
	})
	mustInsert(t, db, &keppel.Repository{
		Name:        "repo1",
		AccountName: "test1",
	})
	for idx := 1; idx <= 10; idx++ {
		pushedAt := time.Unix(int64(10000+10*idx), 0)
		mustInsert(t, db, &keppel.Manifest{
			RepositoryID:        1,
			Digest:              deterministicDummyDigest(idx),
			MediaType:           "",
			SizeBytes:           uint64(1000 * idx),
			PushedAt:            pushedAt,
			ValidatedAt:         pushedAt,
			VulnerabilityStatus: clair.PendingVulnerabilityStatus,
		})
	}
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/quotas/tenant1",
		Header:       map[string]string{"X-Test-Perms": "viewquota:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"manifests": assert.JSONObject{"quota": 100, "usage": 10},
		},
	}.Check(t, r)

	//PUT error cases
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/quotas/tenant1",
		Header: map[string]string{"X-Test-Perms": "viewquota:tenant1"},
		Body: assert.JSONObject{
			"manifests": assert.JSONObject{"quota": 100},
		},
		ExpectStatus: http.StatusForbidden,
	}.Check(t, r)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/quotas/tenant1",
		Header: map[string]string{"X-Test-Perms": "changequota:tenant1"},
		Body: assert.JSONObject{
			"manifests": assert.JSONObject{"quota": 100, "usage": 10},
		},
		ExpectStatus: http.StatusBadRequest,
		ExpectBody:   assert.StringData("request body is not valid JSON: json: unknown field \"usage\"\n"),
	}.Check(t, r)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/quotas/tenant1",
		Header: map[string]string{"X-Test-Perms": "changequota:tenant1"},
		Body: assert.JSONObject{
			"manifests": assert.JSONObject{"quota": 5},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("requested manifest quota (5) is below usage (10)\n"),
	}.Check(t, r)

	//TODO audit events
	_ = auditor
}
