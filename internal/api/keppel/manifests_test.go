// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppelv1_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/majewsky/gg/jsonmatch"
	. "github.com/majewsky/gg/option"
	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/httptest"
	"github.com/sapcc/go-bits/must"
	"go.podman.io/image/v5/manifest"

	"github.com/sapcc/keppel/internal/drivers/basic"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
	"github.com/sapcc/keppel/internal/trivy"
)

func deterministicDummyVulnStatus(counter int) models.VulnerabilityStatus {
	if counter%5 == 0 {
		return models.PendingVulnerabilityStatus
	}
	if counter%3 == 0 {
		return models.HighSeverity
	}
	return models.CleanSeverity
}

func TestManifestsAPI(t *testing.T) {
	test.WithRoundTripper(func(tt *test.RoundTripper) {
		s := test.NewSetup(t, test.WithKeppelAPI, test.WithTrivyDouble,
			test.WithAccount(models.Account{Name: "test1", AuthTenantID: "tenant1"}),
			test.WithAccount(models.Account{Name: "test2", AuthTenantID: "tenant2"}))
		h := s.Handler
		ctx := t.Context()

		// setup test repos (`repo1-2` and `repo2-1` only exist to validate that we
		// don't accidentally list manifests from there)
		repos := []models.Repository{
			{Name: "repo1-1", AccountName: "test1"},
			{Name: "repo1-2", AccountName: "test1"},
			{Name: "repo2-1", AccountName: "test2"},
		}
		for i := range repos {
			must.SucceedT(t, s.DB.Insert(&repos[i]))
		}

		// test empty GET
		h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories/repo1-1/_manifests",
			withPerms("view:tenant1,pull:tenant1"),
		).ExpectJSON(t, http.StatusOK, jsonmatch.Object{"manifests": []jsonmatch.Object{}})

		// test that Keppel API does not allow domain-remapping
		h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories/repo1-1/_manifests",
			withPerms("view:tenant1,pull:tenant1"),
			httptest.WithHeader("X-Forwarded-Host", "test1.registry.example.org"),
			httptest.WithHeader("X-Forwarded-Proto", "https"),
		).ExpectText(t, http.StatusMethodNotAllowed,
			"GET /keppel/v1/accounts/test1/repositories/repo1-1/_manifests endpoint is not supported on domain-remapped APIs\n")

		// insert some dummy manifests and tags into each repo
		for repoID := 1; repoID <= 3; repoID++ {
			repo := repos[repoID-1]

			for idx := 1; idx <= 10; idx++ {
				dummyDigest := test.DeterministicDummyDigest(repoID*10 + idx)
				dummySubject := test.DeterministicDummyDigest(repoID*10 + idx + 1)
				sizeBytes := 1000 * idx
				pushedAt := time.Unix(int64(1000*(repoID*10+idx)), 0)

				dbManifest := models.Manifest{
					RepositoryID:      int64(repoID),
					Digest:            dummyDigest,
					MediaType:         manifest.DockerV2Schema2MediaType,
					SizeBytes:         uint64(sizeBytes),
					PushedAt:          pushedAt,
					NextValidationAt:  pushedAt.Add(models.ManifestValidationInterval),
					LabelsJSON:        `{"foo":"is there"}`,
					GCStatusJSON:      `{"protected_by_recent_upload":true}`,
					MinLayerCreatedAt: Some(time.Unix(20001, 0)),
					MaxLayerCreatedAt: Some(time.Unix(20002, 0)),
					SubjectDigest:     dummySubject,
				}
				if idx == 1 {
					dbManifest.LastPulledAt = Some(pushedAt.Add(100 * time.Second))
				}
				must.SucceedT(t, s.DB.Insert(&dbManifest))

				must.SucceedT(t, s.SD.WriteManifest(
					s.Ctx,
					must.ReturnT(keppel.FindReducedAccount(s.DB, repo.AccountName))(t),
					repo.Name, dummyDigest, []byte(strings.Repeat("x", sizeBytes)),
				))
				must.SucceedT(t, s.DB.Insert(&models.TrivySecurityInfo{
					RepositoryID:        int64(repoID),
					Digest:              dummyDigest,
					VulnerabilityStatus: deterministicDummyVulnStatus(idx),
					NextCheckAt:         Some(time.Unix(0, 0)),
				}))
			}
			// one manifest is referenced by two tags, one is referenced by one tag
			must.SucceedT(t, s.DB.Insert(&models.Tag{
				RepositoryID: int64(repoID),
				Name:         "first",
				Digest:       test.DeterministicDummyDigest(repoID*10 + 1),
				PushedAt:     time.Unix(20001, 0),
				LastPulledAt: Some(time.Unix(20101, 0)),
			}))
			must.SucceedT(t, s.DB.Insert(&models.Tag{
				RepositoryID: int64(repoID),
				Name:         "stillfirst",
				Digest:       test.DeterministicDummyDigest(repoID*10 + 1),
				PushedAt:     time.Unix(20002, 0),
				LastPulledAt: None[time.Time](),
			}))
			must.SucceedT(t, s.DB.Insert(&models.Tag{
				RepositoryID: int64(repoID),
				Name:         "second",
				Digest:       test.DeterministicDummyDigest(repoID*10 + 2),
				PushedAt:     time.Unix(20003, 0),
				LastPulledAt: None[time.Time](),
			}))
		}

		// the results will only include the tags and manifests for `repoID == 1`
		// because we're asking for the repo "test1/repo1-1"
		renderedManifests := make([]jsonmatch.Object, 10)
		for idx := 1; idx <= 10; idx++ {
			renderedManifests[idx-1] = jsonmatch.Object{
				"digest":                          test.DeterministicDummyDigest(10 + idx),
				"media_type":                      manifest.DockerV2Schema2MediaType,
				"size_bytes":                      uint64(1000 * idx),
				"pushed_at":                       int64(1000 * (10 + idx)),
				"last_pulled_at":                  nil,
				"labels":                          jsonmatch.Object{"foo": "is there"},
				"gc_status":                       jsonmatch.Object{"protected_by_recent_upload": true},
				"vulnerability_status":            string(deterministicDummyVulnStatus(idx)),
				"vulnerability_status_changed_at": nil,
				"min_layer_created_at":            20001,
				"max_layer_created_at":            20002,
			}
		}
		renderedManifests[0]["last_pulled_at"] = 11100
		renderedManifests[0]["tags"] = []jsonmatch.Object{
			{"name": "first", "pushed_at": 20001, "last_pulled_at": 20101},
			{"name": "stillfirst", "pushed_at": 20002, "last_pulled_at": nil},
		}
		renderedManifests[1]["tags"] = []jsonmatch.Object{
			{"name": "second", "pushed_at": 20003, "last_pulled_at": nil},
		}
		sort.Slice(renderedManifests, func(i, j int) bool {
			return renderedManifests[i]["digest"].(digest.Digest) < renderedManifests[j]["digest"].(digest.Digest)
		})

		// test GET without pagination
		h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories/repo1-1/_manifests",
			withPerms("view:tenant1,pull:tenant1"),
		).ExpectJSON(t, http.StatusOK, jsonmatch.Object{"manifests": renderedManifests})
		h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories/repo1-1/_manifests?limit=10",
			withPerms("view:tenant1,pull:tenant1"),
		).ExpectJSON(t, http.StatusOK, jsonmatch.Object{"manifests": renderedManifests})

		// test GET with pagination
		h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories/repo1-1/_manifests?limit=5",
			withPerms("view:tenant1,pull:tenant1"),
		).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"manifests": renderedManifests[0:5],
			"truncated": true,
		})
		h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories/repo1-1/_manifests?limit=5&marker="+renderedManifests[4]["digest"].(digest.Digest).String(),
			withPerms("view:tenant1,pull:tenant1"),
		).ExpectJSON(t, http.StatusOK, jsonmatch.Object{"manifests": renderedManifests[5:10]})
		for idx := range 9 {
			expectedBody := jsonmatch.Object{
				"manifests": []jsonmatch.Object{renderedManifests[idx+1]},
			}
			if idx < 8 {
				expectedBody["truncated"] = true
			}
			h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories/repo1-1/_manifests?limit=1&marker="+renderedManifests[idx]["digest"].(digest.Digest).String(),
				withPerms("view:tenant1,pull:tenant1"),
			).ExpectJSON(t, http.StatusOK, expectedBody)
		}

		// test GET failure cases
		h.RespondTo(ctx, "GET /keppel/v1/accounts/doesnotexist/repositories/repo1-1/_manifests",
			withPerms("view:tenant1,pull:tenant1"),
		).ExpectText(t, http.StatusForbidden, "no permission for repository:doesnotexist/repo1-1:pull\n")
		h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories/doesnotexist/_manifests",
			withPerms("view:tenant1,pull:tenant1"),
		).ExpectStatus(t, http.StatusNotFound)
		h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories/repo1-1/_manifests?limit=foo",
			withPerms("view:tenant1,pull:tenant1"),
		).ExpectText(t, http.StatusBadRequest, "strconv.ParseUint: parsing \"foo\": invalid syntax\n")

		// test DELETE manifest happy case
		easypg.AssertDBContent(t, s.DB.Db, "fixtures/before-delete-manifest.sql")
		h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test1/repositories/repo1-1/_manifests/"+test.DeterministicDummyDigest(11).String(),
			withPerms("view:tenant1,delete:tenant1"),
		).ExpectStatus(t, http.StatusNoContent)
		easypg.AssertDBContent(t, s.DB.Db, "fixtures/after-delete-manifest.sql")

		s.Auditor.ExpectEvents(t, cadf.Event{
			RequestPath: "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests/" + test.DeterministicDummyDigest(11).String(),
			Action:      cadf.DeleteAction,
			Outcome:     "success",
			Reason:      test.CADFReasonOK,
			Target: cadf.Resource{
				Attachments: []cadf.Attachment{{
					Name:    "tags",
					TypeURI: "mime:application/json",
					Content: "[\"first\",\"stillfirst\"]",
				}},
				TypeURI:   "docker-registry/account/repository/manifest",
				Name:      "test1/repo1-1@" + test.DeterministicDummyDigest(11).String(),
				ID:        test.DeterministicDummyDigest(11).String(),
				ProjectID: "tenant1",
			},
		})

		// test DELETE tag happy case
		h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test1/repositories/repo1-2/_tags/stillfirst",
			withPerms("view:tenant1,delete:tenant1"),
		).ExpectStatus(t, http.StatusNoContent)
		easypg.AssertDBContent(t, s.DB.Db, "fixtures/after-delete-tag.sql")

		s.Auditor.ExpectEvents(t, cadf.Event{
			RequestPath: "/keppel/v1/accounts/test1/repositories/repo1-2/_tags/stillfirst",
			Action:      cadf.DeleteAction,
			Outcome:     "success",
			Reason:      test.CADFReasonOK,
			Target: cadf.Resource{
				TypeURI:   "docker-registry/account/repository/tag",
				Name:      "test1/repo1-2:stillfirst",
				ID:        test.DeterministicDummyDigest(21).String(),
				ProjectID: "tenant1",
			},
		})

		// test DELETE manifest failure cases
		h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test2/repositories/repo2-1/_manifests/"+test.DeterministicDummyDigest(31).String(),
			withPerms("delete:tenant1,view:tenant1,pull:tenant1"),
		).ExpectText(t, http.StatusForbidden, "no permission for repository:test2/repo2-1:delete\n")
		h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test1/repositories/repo1-2/_manifests/"+test.DeterministicDummyDigest(21).String(),
			withPerms("view:tenant1,pull:tenant1"),
		).ExpectStatus(t, http.StatusForbidden)
		h.RespondTo(ctx, "DELETE /keppel/v1/accounts/doesnotexist/repositories/repo1-2/_manifests/"+test.DeterministicDummyDigest(11).String(),
			withPerms("delete:tenant1,view:tenant1,pull:tenant1"),
		).ExpectText(t, http.StatusForbidden, "no permission for repository:doesnotexist/repo1-2:delete\n")
		h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test1/repositories/doesnotexist/_manifests/"+test.DeterministicDummyDigest(11).String(),
			withPerms("delete:tenant1,view:tenant1,pull:tenant1"),
		).ExpectStatus(t, http.StatusNotFound)
		h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test1/repositories/repo1-1/_manifests/"+test.DeterministicDummyDigest(11).String(),
			withPerms("view:tenant1,delete:tenant1"),
		).ExpectStatus(t, http.StatusNotFound)
		h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test1/repositories/repo1-1/_manifests/second", // this endpoint only works with digests
			withPerms("view:tenant1,delete:tenant1"),
		).ExpectStatus(t, http.StatusNotFound)
		h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test1/repositories/repo1-1/_manifests/sha256:12345",
			withPerms("view:tenant1,delete:tenant1"),
		).ExpectStatus(t, http.StatusNotFound)

		// test DELETE tag failure cases
		h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test1/repositories/repo1-2/_tags/first",
			withPerms("view:tenant1,pull:tenant1"),
		).ExpectStatus(t, http.StatusForbidden)
		h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test2/repositories/repo2-1/_tags/"+test.DeterministicDummyDigest(31).String(), // this endpoint only works with tags
			withPerms("delete:tenant2,view:tenant2"),
		).ExpectStatus(t, http.StatusNotFound)
		h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test2/repositories/doesnotexist/_tags/first",
			withPerms("delete:tenant2,view:tenant2"),
		).ExpectStatus(t, http.StatusNotFound)
		h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test2/repositories/repo2-1/_tags/doesnotexist",
			withPerms("delete:tenant2,view:tenant2"),
		).ExpectStatus(t, http.StatusNotFound)
	})
}

func TestGetTrivyReport(t *testing.T) {
	test.WithRoundTripper(func(tt *test.RoundTripper) {
		s := test.NewSetup(t,
			test.WithKeppelAPI,
			test.WithQuotas,
			test.WithTrivyDouble,
			test.WithAccount(models.Account{Name: "test1", AuthTenantID: "tenant1"}),
		)
		h := s.Handler
		ctx := t.Context()

		// setup: upload an image and an image list
		repoRef := models.Repository{AccountName: "test1", Name: "foo"}
		image := test.GenerateImage(test.GenerateExampleLayer(1))
		imageManifest := image.MustUpload(t, s, repoRef, "")
		listManifest := test.GenerateImageList(image).MustUpload(t, s, repoRef, "")

		// error case: cannot GET report for an image that has not been uploaded
		endpointFor := func(d digest.Digest) string {
			return "GET /keppel/v1/accounts/test1/repositories/foo/_manifests/" + d.String() + "/trivy_report"
		}
		h.RespondTo(ctx, endpointFor(test.DeterministicDummyDigest(1)),
			withPerms("view:tenant1,pull:tenant1"),
		).ExpectStatus(t, http.StatusNotFound)

		// error case: cannot GET report for an image that does not have scannable layers
		h.RespondTo(ctx, endpointFor(listManifest.Digest),
			withPerms("view:tenant1,pull:tenant1"),
		).ExpectStatus(t, http.StatusMethodNotAllowed)

		// error case: cannot GET report for an image that has not been scanned by the janitor after upload
		h.RespondTo(ctx, endpointFor(imageManifest.Digest),
			withPerms("view:tenant1,pull:tenant1"),
		).ExpectStatus(t, http.StatusMethodNotAllowed)

		// for the scannable image, upload a dummy report to the storage in the same way that CheckTrivySecurityStatusJob would
		buf := fmt.Appendf(nil, `{"dummy":"image %s is clean"}`, imageManifest.Digest.String())
		report := trivy.ReportPayload{
			Format:   "json",
			Contents: io.NopCloser(bytes.NewReader(buf)),
		}
		repo := must.ReturnT(keppel.FindRepositoryByID(s.DB, imageManifest.RepositoryID))(t)
		account := must.ReturnT(keppel.FindReducedAccount(s.DB, repo.AccountName))(t)
		must.SucceedT(t, s.SD.WriteTrivyReport(s.Ctx, account, repo.Name, imageManifest.Digest, report))
		test.MustExec(t, s.DB,
			"UPDATE trivy_security_info SET vuln_status = $1, has_enriched_report = TRUE WHERE digest = $2",
			models.CleanSeverity, imageManifest.Digest.String(),
		)

		// happy case: GET on the default format "json" returns that cached report
		h.RespondTo(ctx, endpointFor(imageManifest.Digest), withPerms("view:tenant1,pull:tenant1")).
			ExpectHeader(t, "Content-Type", "application/json").
			ExpectText(t, http.StatusOK, string(buf))

		// happy case: GET on a different format will speak to the Trivy server directly (hence we need to instruct our double what to return)
		imageRef := models.ImageReference{
			Host:      "registry.example.org",
			RepoName:  repo.FullName(),
			Reference: models.ManifestReference{Digest: imageManifest.Digest},
		}
		s.TrivyDouble.ReportFixtures[imageRef] = "fixtures/trivy-report-spdx.json"
		var expected jsonmatch.Object
		must.SucceedT(t, json.Unmarshal(must.ReturnT(os.ReadFile("fixtures/trivy-report-spdx.json"))(t), &expected))
		h.RespondTo(ctx, endpointFor(imageManifest.Digest)+"?format=spdx-json", withPerms("view:tenant1,pull:tenant1")).
			ExpectHeader(t, "Content-Type", "application/json").
			ExpectJSON(t, http.StatusOK, expected)
	})
}

func TestRateLimitsTrivyReport(t *testing.T) {
	limit := redis_rate.Limit{Rate: 2, Period: time.Minute, Burst: 3}
	rateLimitIntervalSeconds := int(limit.Period.Seconds()) / limit.Rate
	rld := &basic.RateLimitDriver{
		Limits: map[keppel.RateLimitedAction]redis_rate.Limit{
			keppel.TrivyReportRetrieveAction: limit,
		},
	}
	rle := &keppel.RateLimitEngine{Driver: rld}

	test.WithRoundTripper(func(tt *test.RoundTripper) {
		s := test.NewSetup(t,
			test.WithKeppelAPI,
			test.WithTrivyDouble,
			test.WithRateLimitEngine(rle),
			test.WithAccount(models.Account{Name: "test1", AuthTenantID: "tenant1"}),
		)
		h := s.Handler
		ctx := t.Context()

		_ = must.ReturnT(keppel.FindOrCreateRepository(s.DB, "foo", models.AccountName("test1")))(t)

		tokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull,push")

		doTrivyRequest := func() httptest.Response {
			endpoint := fmt.Sprintf("GET /keppel/v1/accounts/test1/repositories/foo/_manifests/%s/trivy_report", test.DeterministicDummyDigest(1))
			return h.RespondTo(ctx, endpoint, httptest.WithHeaders(tokenHeaders))
		}
		expectRateLimited := func(reset, retryAfter int) {
			t.Helper()
			doTrivyRequest().ExpectHeaders(t, http.Header{
				"X-RateLimit-Action":    {string(keppel.TrivyReportRetrieveAction)},
				"X-RateLimit-Limit":     {strconv.Itoa(limit.Burst)},
				"X-RateLimit-Remaining": {"0"},
				"X-RateLimit-Reset":     {strconv.Itoa(reset)},
				"Retry-After":           {strconv.Itoa(retryAfter)},
			}).ExpectJSON(t, http.StatusTooManyRequests, jsonmatch.Object{
				"errors": jsonmatch.Array{jsonmatch.Object{
					"code":    "TOOMANYREQUESTS",
					"message": "too many requests; please slow down",
					"detail":  nil,
				}},
			})
		}

		s.Clock.StepBy(time.Hour)

		// we can always execute 1 request initially, and then we can burst on top of that
		timeElapsedDuringRequests := 0
		for range limit.Burst {
			doTrivyRequest().ExpectText(t, http.StatusNotFound, "not found\n")
			s.Clock.StepBy(time.Second)
			timeElapsedDuringRequests++
		}

		// then the next request should be rate-limited
		expectRateLimited(
			rateLimitIntervalSeconds*limit.Burst-timeElapsedDuringRequests,
			rateLimitIntervalSeconds-limit.Burst,
		)

		// be impatient
		s.Clock.StepBy(time.Duration(29-limit.Burst) * time.Second)
		expectRateLimited(
			rateLimitIntervalSeconds*limit.Burst-29,
			rateLimitIntervalSeconds-29,
		)

		// finally!
		s.Clock.StepBy(time.Second)
		doTrivyRequest().ExpectText(t, http.StatusNotFound, "not found\n")

		// aaaand... we're rate-limited again immediately because we haven't
		// recovered our burst budget yet
		expectRateLimited(
			rateLimitIntervalSeconds*limit.Burst,
			rateLimitIntervalSeconds,
		)
	})
}
