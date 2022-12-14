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

package keppelv1_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/assert"

	"github.com/sapcc/keppel/internal/clair"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestQuotasAPI(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler

	//GET on auth tenant without more specific configuration shows default values
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/quotas/tenant1",
		Header:       map[string]string{"X-Test-Perms": "viewquota:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"manifests": assert.JSONObject{"quota": 0, "usage": 0},
		},
	}.Check(t, h)

	//GET basic error cases
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/quotas/tenant1",
		Header:       map[string]string{"X-Test-Perms": "viewquota:tenant2"},
		ExpectStatus: http.StatusForbidden,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/quotas/tenant1",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusForbidden,
	}.Check(t, h)

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
		}.Check(t, h)

		//only the first pass should generate an audit event
		if pass == 1 {
			s.Auditor.ExpectEvents(t, cadf.Event{
				RequestPath: "/keppel/v1/quotas/tenant1",
				Action:      cadf.UpdateAction,
				Outcome:     "success",
				Reason:      test.CADFReasonOK,
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
			s.Auditor.ExpectEvents(t /*, nothing */)
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
	}.Check(t, h)

	//put some manifests in the DB, check thet GET reflects higher usage
	mustInsert(t, s.DB, &keppel.Account{
		Name:           "test1",
		AuthTenantID:   "tenant1",
		GCPoliciesJSON: "[]",
	})
	mustInsert(t, s.DB, &keppel.Repository{
		Name:        "repo1",
		AccountName: "test1",
	})
	for idx := 1; idx <= 10; idx++ {
		pushedAt := time.Unix(int64(10000+10*idx), 0)
		mustInsert(t, s.DB, &keppel.Manifest{
			RepositoryID: 1,
			Digest:       deterministicDummyDigest(idx),
			MediaType:    "",
			SizeBytes:    uint64(1000 * idx),
			PushedAt:     pushedAt,
			ValidatedAt:  pushedAt,
		})
		mustInsert(t, s.DB, &keppel.VulnerabilityInfo{
			RepositoryID: 1,
			Digest:       deterministicDummyDigest(idx),
			Status:       clair.PendingVulnerabilityStatus,
			NextCheckAt:  time.Unix(0, 0),
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
	}.Check(t, h)

	//PUT error cases
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/quotas/tenant1",
		Header: map[string]string{"X-Test-Perms": "viewquota:tenant1"},
		Body: assert.JSONObject{
			"manifests": assert.JSONObject{"quota": 100},
		},
		ExpectStatus: http.StatusForbidden,
	}.Check(t, h)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/quotas/tenant1",
		Header: map[string]string{"X-Test-Perms": "changequota:tenant1"},
		Body: assert.JSONObject{
			"manifests": assert.JSONObject{"quota": 100, "usage": 10},
		},
		ExpectStatus: http.StatusBadRequest,
		ExpectBody:   assert.StringData("request body is not valid JSON: json: unknown field \"usage\"\n"),
	}.Check(t, h)
	assert.HTTPRequest{
		Method: "PUT",
		Path:   "/keppel/v1/quotas/tenant1",
		Header: map[string]string{"X-Test-Perms": "changequota:tenant1"},
		Body: assert.JSONObject{
			"manifests": assert.JSONObject{"quota": 5},
		},
		ExpectStatus: http.StatusUnprocessableEntity,
		ExpectBody:   assert.StringData("requested manifest quota (5) is below usage (10)\n"),
	}.Check(t, h)

	//TODO audit events
}
