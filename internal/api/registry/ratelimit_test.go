// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/sapcc/go-bits/httptest"
	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/drivers/basic"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestRateLimits(t *testing.T) {
	ctx := t.Context()

	limit := redis_rate.Limit{Rate: 2, Period: time.Minute, Burst: 3}
	rateLimitIntervalSeconds := int(limit.Period.Seconds()) / limit.Rate
	rld := &basic.RateLimitDriver{
		Limits: map[keppel.RateLimitedAction]redis_rate.Limit{
			keppel.BlobPullAction:     limit,
			keppel.BlobPushAction:     limit,
			keppel.ManifestPullAction: limit,
			keppel.ManifestPushAction: limit,
		},
	}
	rle := &keppel.RateLimitEngine{Driver: rld, Client: nil}
	setupOptions := []test.SetupOption{
		test.WithRateLimitEngine(rle),
	}

	testWithPrimary(t, setupOptions, func(s test.Setup) {
		// create the "test1/foo" repository to ensure that we don't just always hit NAME_UNKNOWN errors
		_ = must.ReturnT(keppel.FindOrCreateRepository(s.DB, "foo", models.AccountName("test1")))(t)

		tokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull,push")
		bogusDigest := test.DeterministicDummyDigest(1).String()

		// prepare some test requests that should be affected by rate limiting
		// (some of these fail with 404 or 400, but that's okay; the important part is
		// whether they fail with 429 or not)
		type testRequest struct {
			MethodAndPath   string
			RateLimitAction keppel.RateLimitedAction
			OnSuccess       func(httptest.Response)
		}
		testRequests := []testRequest{
			{
				MethodAndPath:   "GET /v2/test1/foo/blobs/" + bogusDigest,
				RateLimitAction: keppel.BlobPullAction,
				OnSuccess: func(resp httptest.Response) {
					resp.ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrBlobUnknown))
				},
			},
			{
				MethodAndPath:   "POST /v2/test1/foo/blobs/uploads/",
				RateLimitAction: keppel.BlobPushAction,
				OnSuccess: func(resp httptest.Response) {
					resp.ExpectStatus(t, http.StatusAccepted)
				},
			},
			{
				MethodAndPath:   "GET /v2/test1/foo/manifests/" + bogusDigest,
				RateLimitAction: keppel.ManifestPullAction,
				OnSuccess: func(resp httptest.Response) {
					resp.ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrManifestUnknown))
				},
			},
			{
				MethodAndPath:   "PUT /v2/test1/foo/manifests/" + bogusDigest,
				RateLimitAction: keppel.ManifestPushAction,
				OnSuccess: func(resp httptest.Response) {
					resp.ExpectJSON(t, http.StatusBadRequest, test.ErrorCode(keppel.ErrManifestInvalid))
				},
			},
		}

		for _, req := range testRequests {
			s.Clock.StepBy(time.Hour)

			// we can always execute 1 request initially, and then we can burst on top of that
			timeElapsedDuringRequests := 0
			for range limit.Burst {
				s.RespondTo(ctx, req.MethodAndPath, httptest.WithHeaders(tokenHeaders)).
					ExpectHeader(t, "X-RateLimit-Action", string(req.RateLimitAction)).
					Expect(req.OnSuccess)
				s.Clock.StepBy(time.Second)
				timeElapsedDuringRequests++
			}

			// then the next request should be rate-limited
			s.RespondTo(ctx, req.MethodAndPath, httptest.WithHeaders(tokenHeaders)).ExpectHeaders(t, http.Header{
				"X-RateLimit-Action":    {string(req.RateLimitAction)},
				"X-RateLimit-Limit":     {strconv.Itoa(limit.Burst)},
				"X-RateLimit-Remaining": {"0"},
				"X-RateLimit-Reset":     {strconv.Itoa(rateLimitIntervalSeconds*limit.Burst - timeElapsedDuringRequests)},
				"Retry-After":           {strconv.Itoa(rateLimitIntervalSeconds - limit.Burst)},
			}).ExpectJSON(t, http.StatusTooManyRequests, test.ErrorCode(keppel.ErrTooManyRequests))

			// be impatient
			s.Clock.StepBy(time.Duration(29-limit.Burst) * time.Second)
			s.RespondTo(ctx, req.MethodAndPath, httptest.WithHeaders(tokenHeaders)).ExpectHeaders(t, http.Header{
				"X-RateLimit-Action":    {string(req.RateLimitAction)},
				"X-RateLimit-Limit":     {strconv.Itoa(limit.Burst)},
				"X-RateLimit-Remaining": {"0"},
				"X-RateLimit-Reset":     {strconv.Itoa(rateLimitIntervalSeconds*limit.Burst - 29)},
				"Retry-After":           {strconv.Itoa(rateLimitIntervalSeconds - 29)},
			}).ExpectJSON(t, http.StatusTooManyRequests, test.ErrorCode(keppel.ErrTooManyRequests))

			// finally!
			s.Clock.StepBy(time.Second)
			s.RespondTo(ctx, req.MethodAndPath, httptest.WithHeaders(tokenHeaders)).
				ExpectHeader(t, "X-RateLimit-Action", string(req.RateLimitAction)).
				Expect(req.OnSuccess)

			// aaaand... we're rate-limited again immediately because we haven't
			// recovered our burst budget yet
			s.RespondTo(ctx, req.MethodAndPath, httptest.WithHeaders(tokenHeaders)).ExpectHeaders(t, http.Header{
				"X-RateLimit-Action":    {string(req.RateLimitAction)},
				"X-RateLimit-Limit":     {strconv.Itoa(limit.Burst)},
				"X-RateLimit-Remaining": {"0"},
				"X-RateLimit-Reset":     {strconv.Itoa(rateLimitIntervalSeconds * limit.Burst)},
				"Retry-After":           {strconv.Itoa(rateLimitIntervalSeconds)},
			}).ExpectJSON(t, http.StatusTooManyRequests, test.ErrorCode(keppel.ErrTooManyRequests))
		}
	})
}

func TestAnycastRateLimits(t *testing.T) {
	ctx := t.Context()
	blob := test.NewBytes([]byte("the blob for our test case"))

	// set up rate limit such that we can pull this blob only twice in a row
	limit := redis_rate.Limit{Rate: len(blob.Contents) * 2, Period: time.Minute, Burst: len(blob.Contents) * 2}

	rld := &basic.RateLimitDriver{
		Limits: map[keppel.RateLimitedAction]redis_rate.Limit{
			keppel.AnycastBlobBytePullAction: limit,
			// all other rate limits are set to "unlimited"
		},
	}
	rle := &keppel.RateLimitEngine{Driver: rld, Client: nil}
	setupOptions := []test.SetupOption{
		test.WithRateLimitEngine(rle),
	}

	testWithPrimary(t, setupOptions, func(s test.Setup) {
		if !currentlyWithAnycast {
			return
		}

		// upload the test blob
		blob.MustUpload(t, s, fooRepoRef)

		// pull it via anycast
		testWithReplica(t, s, "on_first_use", func(firstPass bool, s2 test.Setup) {
			s.Clock.StepBy(time.Hour) // reset all rate limits
			testAnycast(t, firstPass, s2.DB, func() {
				anycastTokenHeaders := s.GetAnycastTokenHeaders(t, "repository:test1/foo:pull")

				// two pulls are allowed by the rate limit (note that these are actually
				// four requests because each expectBlobExists() does one GET and one
				// HEAD, but the rate limit only counts GETs since the rate limit is on
				// the blob contents, which don't get transferred during HEAD)
				expectBlobExists(t, s2, anycastTokenHeaders, "test1/foo", blob)
				expectBlobExists(t, s2, anycastTokenHeaders, "test1/foo", blob)

				// third pull will be rejected by the rate limit
				s2.RespondTo(ctx, "GET /v2/test1/foo/blobs/"+blob.Digest.String(),
					httptest.WithHeaders(anycastTokenHeaders),
				).ExpectHeaders(t, http.Header{
					"X-RateLimit-Action":    {string(keppel.AnycastBlobBytePullAction)},
					"X-RateLimit-Limit":     {strconv.Itoa(limit.Burst)},
					"X-RateLimit-Remaining": {"0"},
					"X-RateLimit-Reset":     {strconv.Itoa(int(limit.Period.Seconds()))},
					"Retry-After":           {"30"},
				}).ExpectJSON(t, http.StatusTooManyRequests, test.ErrorCode(keppel.ErrTooManyRequests))

				// pull from primary is okay since we don't traverse regions
				expectBlobExists(t, s, anycastTokenHeaders, "test1/foo", blob)
			})
		})
	})
}

func TestRateLimitRounding(t *testing.T) {
	ctx := t.Context()

	// NOTE: this must be that high as we otherwise do not observe rounding errors when calculating the rate limit values
	limit := redis_rate.Limit{Rate: 1000, Period: time.Minute, Burst: 1200}
	rld := &basic.RateLimitDriver{
		Limits: map[keppel.RateLimitedAction]redis_rate.Limit{
			keppel.BlobPullAction:     limit,
			keppel.BlobPushAction:     limit,
			keppel.ManifestPullAction: limit,
			keppel.ManifestPushAction: limit,
		},
	}
	rle := &keppel.RateLimitEngine{Driver: rld, Client: nil}
	setupOptions := []test.SetupOption{
		test.WithRateLimitEngine(rle),
		test.WithRepo(models.Repository{AccountName: "test1", Name: "foo"}),
	}

	testWithPrimary(t, setupOptions, func(s test.Setup) {
		methodAndPath := "GET /v2/test1/foo/blobs/" + test.DeterministicDummyDigest(1).String()
		tokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull,push")

		for i := 1; i <= limit.Burst; i++ {
			s.RespondTo(ctx, methodAndPath, httptest.WithHeaders(tokenHeaders)).
				ExpectHeader(t, "X-RateLimit-Action", string(keppel.BlobPullAction)).
				ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrBlobUnknown))
		}

		// next request should be rate-limited
		s.RespondTo(ctx, methodAndPath,
			httptest.WithHeaders(tokenHeaders),
		).ExpectHeaders(t, http.Header{
			"X-RateLimit-Action":    {string(keppel.BlobPullAction)},
			"X-RateLimit-Limit":     {strconv.Itoa(limit.Burst)},
			"X-RateLimit-Remaining": {"0"},
			"X-RateLimit-Reset":     {"72"},
			// we used to see a value of 0 here because the rate limit refills very quickly, and thus the wait is significantly shorter than 0.5 seconds, which was rounded to 0
			"Retry-After": {"1"},
		}).ExpectJSON(t, http.StatusTooManyRequests, test.ErrorCode(keppel.ErrTooManyRequests))
	})
}
