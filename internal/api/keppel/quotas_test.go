// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppelv1_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/majewsky/gg/jsonmatch"
	. "github.com/majewsky/gg/option"
	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/httptest"
	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestQuotasAPI(t *testing.T) {
	// NOTE: This tests both the Keppel-native quota API and the LIQUID API which accesses the same logic.
	s := test.NewSetup(t, test.WithKeppelAPI)
	ctx := t.Context()

	// GET on auth tenant without more specific configuration shows default values
	s.RespondTo(ctx, "GET /keppel/v1/quotas/tenant1", withPerms("viewquota:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"manifests": jsonmatch.Object{"quota": 0, "usage": 0},
		})
	buildLiquidResponse := func(quota, usage uint64) jsonmatch.Object {
		return jsonmatch.Object{
			"infoVersion": 2,
			"resources": map[string]jsonmatch.Object{
				"images": {
					"forbidden": false,
					"quota":     quota,
					"perAZ": map[string]jsonmatch.Object{
						"any": {
							"usage": usage,
						},
					},
				},
			},
		}
	}
	s.RespondTo(ctx, "POST /liquid/v1/projects/tenant1/report-usage",
		withPerms("viewquota:tenant1"),
		httptest.WithJSONBody(map[string]any{"allAZs": []string{"dummy"}}),
	).ExpectJSON(t, http.StatusOK, buildLiquidResponse(0, 0))

	// GET basic error cases: no permission on the respective auth tenant
	s.RespondTo(ctx, "GET /keppel/v1/quotas/tenant1", withPerms("viewquota:tenant2")).
		ExpectStatus(t, http.StatusForbidden)
	s.RespondTo(ctx, "POST /liquid/v1/projects/tenant1/report-usage",
		withPerms("viewquota:tenant2"),
		httptest.WithJSONBody(map[string]any{"allAZs": []string{"dummy"}}),
	).ExpectStatus(t, http.StatusForbidden)

	// GET basic error cases: wrong permission on the respective auth tenant
	s.RespondTo(ctx, "GET /keppel/v1/quotas/tenant1", withPerms("view:tenant1")).
		ExpectStatus(t, http.StatusForbidden)
	s.RespondTo(ctx, "POST /liquid/v1/projects/tenant1/report-usage",
		withPerms("view:tenant1"),
		httptest.WithJSONBody(map[string]any{"allAZs": []string{"dummy"}}),
	).ExpectStatus(t, http.StatusForbidden)

	// PUT happy case with native API
	for _, pass := range []int{1, 2, 3} {
		s.RespondTo(ctx, "PUT /keppel/v1/quotas/tenant1",
			withPerms("changequota:tenant1"),
			httptest.WithJSONBody(map[string]any{
				"manifests": map[string]any{"quota": 50},
			}),
		).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"manifests": jsonmatch.Object{"quota": 50, "usage": 0},
		})

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
		s.RespondTo(ctx, "PUT /liquid/v1/projects/tenant1/quota",
			withPerms("changequota:tenant1"),
			httptest.WithJSONBody(map[string]any{
				"resources": map[string]any{
					"images": map[string]any{"quota": 100},
				},
			}),
		).ExpectStatus(t, http.StatusNoContent)

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
	s.RespondTo(ctx, "GET /keppel/v1/quotas/tenant1", withPerms("viewquota:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"manifests": jsonmatch.Object{"quota": 100, "usage": 0},
		})
	s.RespondTo(ctx, "POST /liquid/v1/projects/tenant1/report-usage",
		withPerms("viewquota:tenant1"),
		httptest.WithJSONBody(map[string]any{"allAZs": []string{"dummy"}}),
	).ExpectJSON(t, http.StatusOK, buildLiquidResponse(100, 0))

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
	s.RespondTo(ctx, "GET /keppel/v1/quotas/tenant1", withPerms("viewquota:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"manifests": jsonmatch.Object{"quota": 100, "usage": 10},
		})
	s.RespondTo(ctx, "POST /liquid/v1/projects/tenant1/report-usage",
		withPerms("viewquota:tenant1"),
		httptest.WithJSONBody(map[string]any{"allAZs": []string{"dummy"}}),
	).ExpectJSON(t, http.StatusOK, buildLiquidResponse(100, 10))

	// PUT error cases
	s.RespondTo(ctx, "PUT /keppel/v1/quotas/tenant1",
		withPerms("viewquota:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"manifests": map[string]any{"quota": 100},
		}),
	).ExpectStatus(t, http.StatusForbidden)
	s.RespondTo(ctx, "PUT /keppel/v1/quotas/tenant1",
		withPerms("changequota:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"manifests": map[string]any{"quota": 100, "usage": 10},
		}),
	).ExpectText(t, http.StatusBadRequest, "request body is not valid JSON: json: unknown field \"usage\"\n")
	s.RespondTo(ctx, "PUT /keppel/v1/quotas/tenant1",
		withPerms("changequota:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"manifests": map[string]any{"quota": 5},
		}),
	).ExpectText(t, http.StatusUnprocessableEntity, "requested manifest quota (5) is below usage (10)\n")

	// TODO audit events
}
