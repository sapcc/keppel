// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppelv1_test

import (
	"net/http"
	"testing"

	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/httptest"
	"go.xyrillian.de/gg/jsonmatch"

	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestQuotasAPI(t *testing.T) {
	// NOTE: This tests both the Keppel-native quota API and the LIQUID API which access the same logic.
	s := test.NewSetup(t,
		test.WithKeppelAPI,
		test.WithAccount(models.Account{Name: "test1", AuthTenantID: "tenant1"}),
	)
	ctx := t.Context()

	var infoVersion int64

	s.RespondTo(ctx, "GET /liquid/v1/info",
		withPerms("viewquota:tenant1"),
	).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"capacityMetricFamilies": nil,
		"categories":             nil,
		"displayName":            "Container Image Registry",
		"rates":                  nil,
		"resources": jsonmatch.Object{
			"images": jsonmatch.Object{
				"displayName":         "Images",
				"hasCapacity":         false,
				"hasQuota":            true,
				"needsResourceDemand": false,
				"topology":            "flat",
			},
		},
		"usageMetricFamilies": nil,
		"version":             jsonmatch.CaptureField(&infoVersion),
	})

	// GET on auth tenant without more specific configuration shows default values
	s.RespondTo(ctx, "GET /keppel/v1/quotas/tenant1", withPerms("viewquota:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"manifests": jsonmatch.Object{"quota": 0, "usage": 0},
		})
	buildLiquidResponse := func(quota, usage uint64) jsonmatch.Object {
		return jsonmatch.Object{
			"infoVersion": infoVersion,
			"resources": jsonmatch.Object{
				"images": jsonmatch.Object{
					"forbidden": false,
					"quota":     quota,
					"perAZ": jsonmatch.Object{
						"any": jsonmatch.Object{
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

	// basic error cases: no permission on the respective auth tenant
	s.RespondTo(ctx, "GET /keppel/v1/quotas/tenant1", withPerms("viewquota:tenant2")).
		ExpectStatus(t, http.StatusForbidden)
	s.RespondTo(ctx, "POST /liquid/v1/projects/tenant1/report-usage",
		withPerms("viewquota:tenant2"),
		httptest.WithJSONBody(map[string]any{"allAZs": []string{"dummy"}}),
	).ExpectStatus(t, http.StatusForbidden)

	// basic error cases: wrong permission on the respective auth tenant
	s.RespondTo(ctx, "GET /keppel/v1/quotas/tenant1", withPerms("view:tenant1")).
		ExpectStatus(t, http.StatusForbidden)
	s.RespondTo(ctx, "POST /liquid/v1/projects/tenant1/report-usage",
		withPerms("view:tenant1"),
		httptest.WithJSONBody(map[string]any{"allAZs": []string{"dummy"}}),
	).ExpectStatus(t, http.StatusForbidden)

	// basic error cases: trying to set bytes quota but it is not enabled
	s.RespondTo(ctx, "PUT /keppel/v1/quotas/tenant1",
		withPerms("changequota:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"bytes": map[string]any{"quota": 50},
		}),
	).ExpectBody(t, http.StatusUnprocessableEntity, []byte("request does not contain manifest quota\n"))
	s.RespondTo(ctx, "PUT /liquid/v1/projects/tenant1/quota",
		withPerms("changequota:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"resources": map[string]any{
				"capacity": map[string]any{"quota": 100},
			},
		}),
	).ExpectBody(t, http.StatusUnprocessableEntity, []byte("request does not contain manifest quota\n"))

	// basic error cases: trying to set bytes quota but it is not enabled
	s.RespondTo(ctx, "PUT /liquid/v1/projects/tenant1/quota",
		withPerms("changequota:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"resources": map[string]any{
				"capacity": map[string]any{"quota": 100},
				"images":   map[string]any{"quota": 100},
			},
		}),
	).ExpectBody(t, http.StatusUnprocessableEntity, []byte("bytes quota is not enabled, but request contains bytes quota\n"))

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
							Content: `{"bytes":9223372036854775807,"manifests":0}`,
						},
						{
							Name:    "payload",
							TypeURI: "mime:application/json",
							Content: `{"bytes":9223372036854775807,"manifests":50}`,
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
							Content: `{"bytes":9223372036854775807,"manifests":50}`,
						},
						{
							Name:    "payload",
							TypeURI: "mime:application/json",
							Content: `{"bytes":9223372036854775807,"manifests":100}`,
						},
					},
				},
			})
		} else {
			s.Auditor.ExpectEvents(t /*, nothing */)
		}
	}

	// reflects changes
	s.RespondTo(ctx, "GET /keppel/v1/quotas/tenant1", withPerms("viewquota:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"manifests": jsonmatch.Object{"quota": 100, "usage": 0},
		})
	s.RespondTo(ctx, "POST /liquid/v1/projects/tenant1/report-usage",
		withPerms("viewquota:tenant1"),
		httptest.WithJSONBody(map[string]any{"allAZs": []string{"dummy"}}),
	).ExpectJSON(t, http.StatusOK, buildLiquidResponse(100, 0))

	// put some manifests in the DB, check that GET reflects higher usage
	repo := models.Repository{
		AccountName: "test1",
		Name:        "foo",
	}
	for idx := 1; idx <= 10; idx++ {
		image := test.GenerateImage(
			test.GenerateExampleLayer(int64(idx)),
			test.GenerateExampleLayer(int64(idx+1)),
		)
		image.MustUpload(t, s, repo, "latest")
	}
	s.Auditor.IgnoreEventsUntilNow()
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
	s.Auditor.ExpectEvents(t /*, nothing */)

	// It is not possible to do strict unmarshalling when using a custom Unmarshal function inside a nested type like gg.Option does
	// TODO: fix this with json v2?
	// s.RespondTo(ctx, "PUT /keppel/v1/quotas/tenant1",
	// 	withPerms("changequota:tenant1"),
	// 	httptest.WithJSONBody(map[string]any{
	// 		"manifests": map[string]any{"quota": 100, "usage": 10},
	// 	}),
	// ).ExpectText(t, http.StatusBadRequest, "request body is not valid JSON: json: unknown field \"usage\"\n")
	// s.Auditor.ExpectEvents(t /*, nothing */)

	s.RespondTo(ctx, "PUT /keppel/v1/quotas/tenant1",
		withPerms("changequota:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"manifests": map[string]any{"quota": 5},
		}),
	).ExpectText(t, http.StatusUnprocessableEntity, "requested manifest quota (5) is below usage (10)\n")
	s.Auditor.ExpectEvents(t /*, nothing */)
}

func TestQuotasAPIWithBytes(t *testing.T) {
	// NOTE: This tests both the Keppel-native quota API and the LIQUID API which access the same logic.
	s := test.NewSetup(t,
		test.WithKeppelAPI,
		test.WithBytesQuotas,
		test.WithAccount(models.Account{Name: "test1", AuthTenantID: "tenant1"}),
	)
	ctx := t.Context()

	var infoVersion int64

	s.RespondTo(ctx, "GET /liquid/v1/info",
		withPerms("viewquota:tenant1"),
	).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"capacityMetricFamilies": nil,
		"categories":             nil,
		"displayName":            "Container Image Registry",
		"rates":                  nil,
		"resources": jsonmatch.Object{
			"capacity": jsonmatch.Object{
				"displayName":         "Capacity",
				"hasCapacity":         false,
				"hasQuota":            true,
				"needsResourceDemand": false,
				"topology":            "flat",
				"unit":                "B",
			},
			"images": jsonmatch.Object{
				"displayName":         "Images",
				"hasCapacity":         false,
				"hasQuota":            true,
				"needsResourceDemand": false,
				"topology":            "flat",
			},
		},
		"usageMetricFamilies": nil,
		"version":             jsonmatch.CaptureField(&infoVersion),
	})

	// GET on auth tenant without more specific configuration shows default values
	s.RespondTo(ctx, "GET /keppel/v1/quotas/tenant1", withPerms("viewquota:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"bytes":     jsonmatch.Object{"quota": 0, "usage": 0},
			"manifests": jsonmatch.Object{"quota": 0, "usage": 0},
		})
	buildLiquidResponse := func(capacityQuota, capacityUsage, imagesQuota, imagesUsage uint64) jsonmatch.Object {
		return jsonmatch.Object{
			"infoVersion": infoVersion,
			"resources": jsonmatch.Object{
				"capacity": jsonmatch.Object{
					"forbidden": false,
					"quota":     capacityQuota,
					"perAZ": jsonmatch.Object{
						"any": jsonmatch.Object{
							"usage": capacityUsage,
						},
					},
				},
				"images": jsonmatch.Object{
					"forbidden": false,
					"quota":     imagesQuota,
					"perAZ": jsonmatch.Object{
						"any": jsonmatch.Object{
							"usage": imagesUsage,
						},
					},
				},
			},
		}
	}

	s.RespondTo(ctx, "POST /liquid/v1/projects/tenant1/report-usage",
		withPerms("viewquota:tenant1"),
		httptest.WithJSONBody(map[string]any{"allAZs": []string{"dummy"}}),
	).ExpectJSON(t, http.StatusOK, buildLiquidResponse(0, 0, 0, 0))

	// PUT happy case with native API
	for _, pass := range []int{1, 2, 3} {
		s.RespondTo(ctx, "PUT /keppel/v1/quotas/tenant1",
			withPerms("changequota:tenant1"),
			httptest.WithJSONBody(map[string]any{
				"bytes":     map[string]any{"quota": 5_000_000},
				"manifests": map[string]any{"quota": 50},
			}),
		).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"bytes":     jsonmatch.Object{"quota": 5_000_000, "usage": 0},
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
							Content: `{"bytes":5000000,"manifests":50}`,
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
					"capacity": map[string]any{"quota": 50_000_000},
					"images":   map[string]any{"quota": 100},
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
							Content: `{"bytes":5000000,"manifests":50}`,
						},
						{
							Name:    "payload",
							TypeURI: "mime:application/json",
							Content: `{"bytes":50000000,"manifests":100}`,
						},
					},
				},
			})
		} else {
			s.Auditor.ExpectEvents(t /*, nothing */)
		}
	}

	// reflects changes
	s.RespondTo(ctx, "GET /keppel/v1/quotas/tenant1", withPerms("viewquota:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"bytes":     jsonmatch.Object{"quota": 50_000_000, "usage": 0},
			"manifests": jsonmatch.Object{"quota": 100, "usage": 0},
		})
	s.RespondTo(ctx, "POST /liquid/v1/projects/tenant1/report-usage",
		withPerms("viewquota:tenant1"),
		httptest.WithJSONBody(map[string]any{"allAZs": []string{"dummy"}}),
	).ExpectJSON(t, http.StatusOK, buildLiquidResponse(50_000_000, 0, 100, 0))

	// put some blobs in the DB, check that GET reflects higher usage
	repo := models.Repository{
		AccountName: "test1",
		Name:        "foo",
	}
	for idx := 1; idx <= 10; idx++ {
		image := test.GenerateImage(
			test.GenerateExampleLayer(int64(idx)),
			test.GenerateExampleLayer(int64(idx+1)),
		)
		image.MustUpload(t, s, repo, "latest")
	}
	s.Auditor.IgnoreEventsUntilNow()

	s.RespondTo(ctx, "GET /keppel/v1/quotas/tenant1", withPerms("viewquota:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"bytes":     jsonmatch.Object{"quota": 50_000_000, "usage": 11555866},
			"manifests": jsonmatch.Object{"quota": 100, "usage": 10},
		})
	s.RespondTo(ctx, "POST /liquid/v1/projects/tenant1/report-usage",
		withPerms("viewquota:tenant1"),
		httptest.WithJSONBody(map[string]any{"allAZs": []string{"dummy"}}),
	).ExpectJSON(t, http.StatusOK, buildLiquidResponse(50000000, 11555866, 100, 10))

	// PUT error cases
	s.RespondTo(ctx, "PUT /keppel/v1/quotas/tenant1",
		withPerms("viewquota:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"bytes":     map[string]any{"bytes": 50_000_000, "quota": 5_000_000},
			"manifests": map[string]any{"bytes": 10000, "quota": 100},
		}),
	).ExpectStatus(t, http.StatusForbidden)
	s.Auditor.ExpectEvents(t /*, nothing */)

	// trying to set quota without bytes when bytes quota is enabled
	s.RespondTo(ctx, "PUT /keppel/v1/quotas/tenant1",
		withPerms("changequota:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"manifests": map[string]any{"quota": 100},
		}),
	).ExpectBody(t, http.StatusUnprocessableEntity, []byte("bytes quota is enabled, but request does not contain bytes quota\n"))
	s.Auditor.ExpectEvents(t /*, nothing */)
	s.RespondTo(ctx, "PUT /liquid/v1/projects/tenant1/quota",
		withPerms("changequota:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"resources": map[string]any{
				"images": map[string]any{"quota": 100},
			},
		}),
	).ExpectBody(t, http.StatusUnprocessableEntity, []byte("bytes quota is enabled, but request does not contain bytes quota\n"))
	s.Auditor.ExpectEvents(t /*, nothing */)

	s.RespondTo(ctx, "PUT /keppel/v1/quotas/tenant1",
		withPerms("changequota:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"bytes":     map[string]any{"quota": 5_000_000},
			"manifests": map[string]any{"quota": 5},
		}),
	).ExpectText(t, http.StatusUnprocessableEntity, "requested manifest quota (5) is below usage (10)\n")
	s.Auditor.ExpectEvents(t /*, nothing */)

	// trying to set bytes quota below current usage
	s.RespondTo(ctx, "PUT /keppel/v1/quotas/tenant1",
		withPerms("changequota:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"bytes":     map[string]any{"quota": 1_000_000},
			"manifests": map[string]any{"quota": 100},
		}),
	).ExpectText(t, http.StatusUnprocessableEntity, "requested bytes quota (1000000) is below usage (11555866)\n")
	s.Auditor.ExpectEvents(t /*, nothing */)
	s.RespondTo(ctx, "PUT /liquid/v1/projects/tenant1/quota",
		withPerms("changequota:tenant1"),
		httptest.WithJSONBody(map[string]any{
			"resources": map[string]any{
				"capacity": map[string]any{"quota": 1_000_000},
				"images":   map[string]any{"quota": 100},
			},
		}),
	).ExpectText(t, http.StatusUnprocessableEntity, "requested bytes quota (1000000) is below usage (11555866)\n")
	s.Auditor.ExpectEvents(t /*, nothing */)
}
