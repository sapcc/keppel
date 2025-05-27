// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppelv1_test

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/containers/image/v5/manifest"
	"github.com/go-redis/redis_rate/v10"
	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"

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

		// setup test repos (`repo1-2` and `repo2-1` only exist to validate that we
		// don't accidentally list manifests from there)
		repos := []*models.Repository{
			{Name: "repo1-1", AccountName: "test1"},
			{Name: "repo1-2", AccountName: "test1"},
			{Name: "repo2-1", AccountName: "test2"},
		}
		for _, repo := range repos {
			test.MustInsert(t, s.DB, repo)
		}

		// test empty GET
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests",
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,pull:tenant1"},
			ExpectStatus: http.StatusOK,
			ExpectBody:   assert.JSONObject{"manifests": []assert.JSONObject{}},
		}.Check(t, h)

		// test that Keppel API does not allow domain-remapping
		assert.HTTPRequest{
			Method: "GET",
			Path:   "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests",
			Header: map[string]string{
				"X-Test-Perms":      "view:tenant1,pull:tenant1",
				"X-Forwarded-Host":  "test1.registry.example.org",
				"X-Forwarded-Proto": "https",
			},
			ExpectStatus: http.StatusMethodNotAllowed,
			ExpectBody:   assert.StringData("GET /keppel/v1/accounts/test1/repositories/repo1-1/_manifests endpoint is not supported on domain-remapped APIs\n"),
		}.Check(t, h)

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
					SizeBytes:         uint64(sizeBytes), //nolint:gosec // construction guarantees that value is positive
					PushedAt:          pushedAt,
					NextValidationAt:  pushedAt.Add(models.ManifestValidationInterval),
					LabelsJSON:        `{"foo":"is there"}`,
					GCStatusJSON:      `{"protected_by_recent_upload":true}`,
					MinLayerCreatedAt: p2time(time.Unix(20001, 0)),
					MaxLayerCreatedAt: p2time(time.Unix(20002, 0)),
					SubjectDigest:     dummySubject,
				}
				if idx == 1 {
					dbManifest.LastPulledAt = p2time(pushedAt.Add(100 * time.Second))
				}
				test.MustInsert(t, s.DB, &dbManifest)

				test.MustDo(t, s.SD.WriteManifest(
					s.Ctx,
					models.ReducedAccount{Name: repo.AccountName},
					repo.Name, dummyDigest, []byte(strings.Repeat("x", sizeBytes)),
				))
				test.MustInsert(t, s.DB, &models.TrivySecurityInfo{
					RepositoryID:        int64(repoID),
					Digest:              dummyDigest,
					VulnerabilityStatus: deterministicDummyVulnStatus(idx),
					NextCheckAt:         time.Unix(0, 0),
				})
			}
			// one manifest is referenced by two tags, one is referenced by one tag
			test.MustInsert(t, s.DB, &models.Tag{
				RepositoryID: int64(repoID),
				Name:         "first",
				Digest:       test.DeterministicDummyDigest(repoID*10 + 1),
				PushedAt:     time.Unix(20001, 0),
				LastPulledAt: p2time(time.Unix(20101, 0)),
			})
			test.MustInsert(t, s.DB, &models.Tag{
				RepositoryID: int64(repoID),
				Name:         "stillfirst",
				Digest:       test.DeterministicDummyDigest(repoID*10 + 1),
				PushedAt:     time.Unix(20002, 0),
				LastPulledAt: nil,
			})
			test.MustInsert(t, s.DB, &models.Tag{
				RepositoryID: int64(repoID),
				Name:         "second",
				Digest:       test.DeterministicDummyDigest(repoID*10 + 2),
				PushedAt:     time.Unix(20003, 0),
				LastPulledAt: nil,
			})
		}

		// the results will only include the tags and manifests for `repoID == 1`
		// because we're asking for the repo "test1/repo1-1"
		renderedManifests := make([]assert.JSONObject, 10)
		for idx := 1; idx <= 10; idx++ {
			renderedManifests[idx-1] = assert.JSONObject{
				"digest":               test.DeterministicDummyDigest(10 + idx),
				"media_type":           manifest.DockerV2Schema2MediaType,
				"size_bytes":           uint64(1000 * idx), //nolint:gosec // construction guarantees that value is positive
				"pushed_at":            int64(1000 * (10 + idx)),
				"last_pulled_at":       nil,
				"labels":               assert.JSONObject{"foo": "is there"},
				"gc_status":            assert.JSONObject{"protected_by_recent_upload": true},
				"vulnerability_status": string(deterministicDummyVulnStatus(idx)),
				"min_layer_created_at": 20001,
				"max_layer_created_at": 20002,
			}
		}
		renderedManifests[0]["last_pulled_at"] = 11100
		renderedManifests[0]["tags"] = []assert.JSONObject{
			{"name": "first", "pushed_at": 20001, "last_pulled_at": 20101},
			{"name": "stillfirst", "pushed_at": 20002, "last_pulled_at": nil},
		}
		renderedManifests[1]["tags"] = []assert.JSONObject{
			{"name": "second", "pushed_at": 20003, "last_pulled_at": nil},
		}
		sort.Slice(renderedManifests, func(i, j int) bool {
			return renderedManifests[i]["digest"].(digest.Digest) < renderedManifests[j]["digest"].(digest.Digest)
		})

		// test GET without pagination
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests",
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,pull:tenant1"},
			ExpectStatus: http.StatusOK,
			ExpectBody:   assert.JSONObject{"manifests": renderedManifests},
		}.Check(t, h)
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests?limit=10",
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,pull:tenant1"},
			ExpectStatus: http.StatusOK,
			ExpectBody:   assert.JSONObject{"manifests": renderedManifests},
		}.Check(t, h)

		// test GET with pagination
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests?limit=5",
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,pull:tenant1"},
			ExpectStatus: http.StatusOK,
			ExpectBody: assert.JSONObject{
				"manifests": renderedManifests[0:5],
				"truncated": true,
			},
		}.Check(t, h)
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests?limit=5&marker=" + renderedManifests[4]["digest"].(digest.Digest).String(),
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,pull:tenant1"},
			ExpectStatus: http.StatusOK,
			ExpectBody:   assert.JSONObject{"manifests": renderedManifests[5:10]},
		}.Check(t, h)
		for idx := range 9 {
			expectedBody := assert.JSONObject{
				"manifests": []assert.JSONObject{renderedManifests[idx+1]},
			}
			if idx < 8 {
				expectedBody["truncated"] = true
			}
			assert.HTTPRequest{
				Method:       "GET",
				Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests?limit=1&marker=" + renderedManifests[idx]["digest"].(digest.Digest).String(),
				Header:       map[string]string{"X-Test-Perms": "view:tenant1,pull:tenant1"},
				ExpectStatus: http.StatusOK,
				ExpectBody:   expectedBody,
			}.Check(t, h)
		}

		// test GET failure cases
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/keppel/v1/accounts/doesnotexist/repositories/repo1-1/_manifests",
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,pull:tenant1"},
			ExpectStatus: http.StatusForbidden,
			ExpectBody:   assert.StringData("no permission for repository:doesnotexist/repo1-1:pull\n"),
		}.Check(t, h)
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/keppel/v1/accounts/test1/repositories/doesnotexist/_manifests",
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,pull:tenant1"},
			ExpectStatus: http.StatusNotFound,
		}.Check(t, h)
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests?limit=foo",
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,pull:tenant1"},
			ExpectStatus: http.StatusBadRequest,
			ExpectBody:   assert.StringData("strconv.ParseUint: parsing \"foo\": invalid syntax\n"),
		}.Check(t, h)

		// test DELETE manifest happy case
		easypg.AssertDBContent(t, s.DB.Db, "fixtures/before-delete-manifest.sql")
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests/" + test.DeterministicDummyDigest(11).String(),
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
			ExpectStatus: http.StatusNoContent,
		}.Check(t, h)
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
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/keppel/v1/accounts/test1/repositories/repo1-2/_tags/stillfirst",
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
			ExpectStatus: http.StatusNoContent,
		}.Check(t, h)
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
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/keppel/v1/accounts/test2/repositories/repo2-1/_manifests/" + test.DeterministicDummyDigest(31).String(),
			Header:       map[string]string{"X-Test-Perms": "delete:tenant1,view:tenant1,pull:tenant1"},
			ExpectStatus: http.StatusForbidden,
			ExpectBody:   assert.StringData("no permission for repository:test2/repo2-1:delete\n"),
		}.Check(t, h)
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/keppel/v1/accounts/test1/repositories/repo1-2/_manifests/" + test.DeterministicDummyDigest(21).String(),
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,pull:tenant1"},
			ExpectStatus: http.StatusForbidden,
		}.Check(t, h)
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/keppel/v1/accounts/doesnotexist/repositories/repo1-2/_manifests/" + test.DeterministicDummyDigest(11).String(),
			Header:       map[string]string{"X-Test-Perms": "delete:tenant1,view:tenant1,pull:tenant1"},
			ExpectStatus: http.StatusForbidden,
			ExpectBody:   assert.StringData("no permission for repository:doesnotexist/repo1-2:delete\n"),
		}.Check(t, h)
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/keppel/v1/accounts/test1/repositories/doesnotexist/_manifests/" + test.DeterministicDummyDigest(11).String(),
			Header:       map[string]string{"X-Test-Perms": "delete:tenant1,view:tenant1,pull:tenant1"},
			ExpectStatus: http.StatusNotFound,
		}.Check(t, h)
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests/" + test.DeterministicDummyDigest(11).String(),
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
			ExpectStatus: http.StatusNotFound,
		}.Check(t, h)
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests/second", // this endpoint only works with digests
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
			ExpectStatus: http.StatusNotFound,
		}.Check(t, h)
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests/sha256:12345",
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
			ExpectStatus: http.StatusNotFound,
		}.Check(t, h)

		// test DELETE tag failure cases
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/keppel/v1/accounts/test1/repositories/repo1-2/_tags/first",
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,pull:tenant1"},
			ExpectStatus: http.StatusForbidden,
		}.Check(t, h)
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/keppel/v1/accounts/test2/repositories/repo2-1/_tags/" + test.DeterministicDummyDigest(31).String(), // this endpoint only works with tags
			Header:       map[string]string{"X-Test-Perms": "delete:tenant2,view:tenant2"},
			ExpectStatus: http.StatusNotFound,
		}.Check(t, h)
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/keppel/v1/accounts/test2/repositories/doesnotexist/_tags/first",
			Header:       map[string]string{"X-Test-Perms": "delete:tenant2,view:tenant2"},
			ExpectStatus: http.StatusNotFound,
		}.Check(t, h)
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/keppel/v1/accounts/test2/repositories/repo2-1/_tags/doesnotexist",
			Header:       map[string]string{"X-Test-Perms": "delete:tenant2,view:tenant2"},
			ExpectStatus: http.StatusNotFound,
		}.Check(t, h)
	})
}

func p2time(x time.Time) *time.Time {
	return &x
}

func TestGetTrivyReport(t *testing.T) {
	test.WithRoundTripper(func(tt *test.RoundTripper) {
		s := test.NewSetup(t,
			test.WithKeppelAPI,
			test.WithQuotas,
			test.WithTrivyDouble,
			test.WithAccount(models.Account{Name: "test1", AuthTenantID: "tenant1"}),
		)

		// setup: upload an image and an image list
		repoRef := models.Repository{AccountName: "test1", Name: "foo"}
		image := test.GenerateImage(test.GenerateExampleLayer(1))
		imageManifest := image.MustUpload(t, s, repoRef, "")
		listManifest := test.GenerateImageList(image).MustUpload(t, s, repoRef, "")

		// error case: cannot GET report for an image that has not been uploaded
		endpointFor := func(d digest.Digest) string {
			return "/keppel/v1/accounts/test1/repositories/foo/_manifests/" + d.String() + "/trivy_report"
		}
		assert.HTTPRequest{
			Method:       "GET",
			Path:         endpointFor(test.DeterministicDummyDigest(1)),
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,pull:tenant1"},
			ExpectStatus: http.StatusNotFound,
		}.Check(t, s.Handler)

		// error case: cannot GET report for an image that does not have scannable layers
		assert.HTTPRequest{
			Method:       "GET",
			Path:         endpointFor(listManifest.Digest),
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,pull:tenant1"},
			ExpectStatus: http.StatusMethodNotAllowed,
		}.Check(t, s.Handler)

		// error case: cannot GET report for an image that has not been scanned by the janitor after upload
		assert.HTTPRequest{
			Method:       "GET",
			Path:         endpointFor(imageManifest.Digest),
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,pull:tenant1"},
			ExpectStatus: http.StatusMethodNotAllowed,
		}.Check(t, s.Handler)

		// for the scannable image, upload a dummy report to the storage in the same way that CheckTrivySecurityStatusJob would
		report := trivy.ReportPayload{
			Format:   "json",
			Contents: []byte(fmt.Sprintf(`{"dummy":"image %s is clean"}`, imageManifest.Digest.String())),
		}
		repo, err := keppel.FindRepositoryByID(s.DB, imageManifest.RepositoryID)
		test.MustDo(t, err)
		test.MustDo(t, s.SD.WriteTrivyReport(s.Ctx, models.ReducedAccount{Name: repo.AccountName}, repo.Name, imageManifest.Digest, report))
		test.MustExec(t, s.DB,
			"UPDATE trivy_security_info SET vuln_status = $1, has_enriched_report = TRUE WHERE digest = $2",
			models.CleanSeverity, imageManifest.Digest.String(),
		)

		// happy case: GET on the default format "json" returns that cached report
		assert.HTTPRequest{
			Method:       "GET",
			Path:         endpointFor(imageManifest.Digest),
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,pull:tenant1"},
			ExpectStatus: http.StatusOK,
			ExpectHeader: map[string]string{"Content-Type": "application/json"},
			ExpectBody:   assert.ByteData(report.Contents),
		}.Check(t, s.Handler)

		// happy case: GET on a different format will speak to the Trivy server directly (hence we need to instruct our double what to return)
		imageRef := models.ImageReference{
			Host:      "registry.example.org",
			RepoName:  repo.FullName(),
			Reference: models.ManifestReference{Digest: imageManifest.Digest},
		}
		s.TrivyDouble.ReportFixtures[imageRef] = "fixtures/trivy-report-spdx.json"
		assert.HTTPRequest{
			Method:       "GET",
			Path:         endpointFor(imageManifest.Digest) + "?format=spdx-json",
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,pull:tenant1"},
			ExpectStatus: http.StatusOK,
			ExpectHeader: map[string]string{"Content-Type": "application/json"},
			ExpectBody:   assert.JSONFixtureFile("fixtures/trivy-report-spdx.json"),
		}.Check(t, s.Handler)
	})
}

func TestRateLimitsTrivyReport(t *testing.T) {
	limit := redis_rate.Limit{Rate: 2, Period: time.Minute, Burst: 3}
	rld := basic.RateLimitDriver{
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
			test.WithAccount(models.Account{Name: "test1"}),
		)
		h := s.Handler

		_, err := keppel.FindOrCreateRepository(s.DB, "foo", models.AccountName("test1"))
		test.MustDo(t, err)

		token := s.GetToken(t, "repository:test1/foo:pull,push")

		req := assert.HTTPRequest{
			Method:       "GET",
			Path:         fmt.Sprintf("/keppel/v1/accounts/test1/repositories/foo/_manifests/%s/trivy_report", test.DeterministicDummyDigest(1)),
			Header:       map[string]string{"Authorization": "Bearer " + token},
			ExpectStatus: http.StatusNotFound,
			ExpectHeader: map[string]string{},
			ExpectBody:   assert.StringData("not found\n"),
		}

		s.Clock.StepBy(time.Hour)

		// we can always execute 1 request initially, and then we can burst on top of that
		for range limit.Burst {
			req.Check(t, h)
			s.Clock.StepBy(time.Second)
		}

		// then the next request should be rate-limited
		failingReq := req
		failingReq.ExpectBody = test.ErrorCode(keppel.ErrTooManyRequests)
		failingReq.ExpectStatus = http.StatusTooManyRequests
		failingReq.ExpectHeader = map[string]string{
			"Retry-After": strconv.Itoa(30 - limit.Burst),
		}
		failingReq.Check(t, h)

		// be impatient
		s.Clock.StepBy(time.Duration(29-limit.Burst) * time.Second)
		failingReq.ExpectHeader["Retry-After"] = "1"
		failingReq.Check(t, h)

		// finally!
		s.Clock.StepBy(time.Second)
		req.Check(t, h)

		// aaaand... we're rate-limited again immediately because we haven't
		// recovered our burst budget yet
		failingReq.ExpectHeader["Retry-After"] = "30"
		failingReq.Check(t, h)
	})
}
