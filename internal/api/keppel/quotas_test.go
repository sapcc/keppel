// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppelv1_test

import (
	"net/http"
	"testing"
	"time"

	. "github.com/majewsky/gg/option"
	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestQuotasAPI(t *testing.T) {
	// NOTE: This tests both the Keppel-native quota API and the LIQUID API which accesses the same logic.
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler

	// GET on auth tenant without more specific configuration shows default values
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/quotas/tenant1",
		Header:       map[string]string{"X-Test-Perms": "viewquota:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"manifests": assert.JSONObject{"quota": 0, "usage": 0},
		},
	}.Check(t, h)
	buildLiquidResponse := func(quota, usage uint64) assert.JSONObject {
		return assert.JSONObject{
			"infoVersion": 1,
			"resources": map[string]assert.JSONObject{
				"images": {
					"forbidden": false,
					"quota":     quota,
					"perAZ": map[string]assert.JSONObject{
						"any": {
							"usage": usage,
						},
					},
				},
			},
		}
	}
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/liquid/v1/projects/tenant1/report-usage",
		Header:       map[string]string{"X-Test-Perms": "viewquota:tenant1"},
		Body:         assert.JSONObject{"allAZs": []string{"dummy"}},
		ExpectStatus: http.StatusOK,
		ExpectBody:   buildLiquidResponse(0, 0),
	}.Check(t, h)

	// GET basic error cases: no permission on the respective auth tenant
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/quotas/tenant1",
		Header:       map[string]string{"X-Test-Perms": "viewquota:tenant2"},
		ExpectStatus: http.StatusForbidden,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/liquid/v1/projects/tenant1/report-usage",
		Header:       map[string]string{"X-Test-Perms": "viewquota:tenant2"},
		Body:         assert.JSONObject{"allAZs": []string{"dummy"}},
		ExpectStatus: http.StatusForbidden,
	}.Check(t, h)

	// GET basic error cases: wrong permission on the respective auth tenant
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/quotas/tenant1",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusForbidden,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/liquid/v1/projects/tenant1/report-usage",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		Body:         assert.JSONObject{"allAZs": []string{"dummy"}},
		ExpectStatus: http.StatusForbidden,
	}.Check(t, h)

	// PUT happy case with native API
	for _, pass := range []int{1, 2, 3} {
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/keppel/v1/quotas/tenant1",
			Header: map[string]string{"X-Test-Perms": "changequota:tenant1"},
			Body: assert.JSONObject{
				"manifests": assert.JSONObject{"quota": 50},
			},
			ExpectStatus: http.StatusOK,
			ExpectBody: assert.JSONObject{
				"manifests": assert.JSONObject{"quota": 50, "usage": 0},
			},
		}.Check(t, h)

		// only the first pass should generate an audit event
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
							Content: `{"manifests":50}`,
						},
					},
				},
			})
		} else {
			s.Auditor.ExpectEvents(t /*, nothing */)
		}
	}

	// PUT happy case with LIQUID API
	for _, pass := range []int{1, 2, 3} {
		assert.HTTPRequest{
			Method: "PUT",
			Path:   "/liquid/v1/projects/tenant1/quota",
			Header: map[string]string{"X-Test-Perms": "changequota:tenant1"},
			Body: assert.JSONObject{
				"resources": map[string]assert.JSONObject{
					"images": {"quota": 100},
				},
			},
			ExpectStatus: http.StatusNoContent,
		}.Check(t, h)

		// only the first pass should generate an audit event
		if pass == 1 {
			s.Auditor.ExpectEvents(t, cadf.Event{
				RequestPath: "/liquid/v1/projects/tenant1/quota",
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
							Content: `{"manifests":50}`,
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

	// GET reflects changes
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/quotas/tenant1",
		Header:       map[string]string{"X-Test-Perms": "viewquota:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"manifests": assert.JSONObject{"quota": 100, "usage": 0},
		},
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/liquid/v1/projects/tenant1/report-usage",
		Header:       map[string]string{"X-Test-Perms": "viewquota:tenant1"},
		Body:         assert.JSONObject{"allAZs": []string{"dummy"}},
		ExpectStatus: http.StatusOK,
		ExpectBody:   buildLiquidResponse(100, 0),
	}.Check(t, h)

	// put some manifests in the DB, check that GET reflects higher usage
	test.MustExec(t, s.DB, `INSERT INTO accounts (name, auth_tenant_id) VALUES ('test1', 'tenant1')`)
	must.SucceedT(t, s.DB.Insert(&models.Repository{
		Name:        "repo1",
		AccountName: "test1",
	}))
	for idx := 1; idx <= 10; idx++ {
		pushedAt := time.Unix(int64(10000+10*idx), 0)
		must.SucceedT(t, s.DB.Insert(&models.Manifest{
			RepositoryID:     1,
			Digest:           test.DeterministicDummyDigest(idx),
			MediaType:        "",
			SizeBytes:        uint64(1000 * idx),
			PushedAt:         pushedAt,
			NextValidationAt: pushedAt.Add(models.ManifestValidationInterval),
		}))
		must.SucceedT(t, s.DB.Insert(&models.TrivySecurityInfo{
			RepositoryID:        1,
			Digest:              test.DeterministicDummyDigest(idx),
			VulnerabilityStatus: models.PendingVulnerabilityStatus,
			NextCheckAt:         Some(time.Unix(0, 0)),
		}))
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
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/liquid/v1/projects/tenant1/report-usage",
		Header:       map[string]string{"X-Test-Perms": "viewquota:tenant1"},
		Body:         assert.JSONObject{"allAZs": []string{"dummy"}},
		ExpectStatus: http.StatusOK,
		ExpectBody:   buildLiquidResponse(100, 10),
	}.Check(t, h)

	// PUT error cases
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

	// TODO audit events
}
