// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/sapcc/go-bits/assert"

	"github.com/sapcc/keppel/internal/drivers/basic"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestRateLimits(t *testing.T) {
	limit := redis_rate.Limit{Rate: 2, Period: time.Minute, Burst: 3}
	rld := basic.RateLimitDriver{
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
		// create the "test1/foo" repository to ensure that we don't just always hit
		// NAME_UNKNOWN errors
		_, err := keppel.FindOrCreateRepository(s.DB, "foo", models.AccountName("test1"))
		test.MustDo(t, err)

		h := s.Handler
		token := s.GetToken(t, "repository:test1/foo:pull,push")
		bogusDigest := test.DeterministicDummyDigest(1).String()

		// prepare some test requests that should be affected by rate limiting
		// (some of these fail with 404 or 400, but that's okay; the important part is
		// whether they fail with 429 or not)
		testRequests := []assert.HTTPRequest{
			{
				Method:       "GET",
				Path:         "/v2/test1/foo/blobs/" + bogusDigest,
				Header:       map[string]string{"Authorization": "Bearer " + token},
				ExpectStatus: http.StatusNotFound,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrBlobUnknown),
			},
			{
				Method:       "POST",
				Path:         "/v2/test1/foo/blobs/uploads/",
				Header:       map[string]string{"Authorization": "Bearer " + token},
				ExpectStatus: http.StatusAccepted,
				ExpectHeader: test.VersionHeader,
			},
			{
				Method:       "GET",
				Path:         "/v2/test1/foo/manifests/" + bogusDigest,
				Header:       map[string]string{"Authorization": "Bearer " + token},
				ExpectStatus: http.StatusNotFound,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrManifestUnknown),
			},
			{
				Method:       "PUT",
				Path:         "/v2/test1/foo/manifests/" + bogusDigest,
				Header:       map[string]string{"Authorization": "Bearer " + token},
				ExpectStatus: http.StatusBadRequest,
				ExpectHeader: test.VersionHeader,
				ExpectBody:   test.ErrorCode(keppel.ErrManifestInvalid),
			},
		}

		for _, req := range testRequests {
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
				test.VersionHeaderKey: test.VersionHeaderValue,
				"Retry-After":         strconv.Itoa(30 - limit.Burst),
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
		}
	})
}

func TestAnycastRateLimits(t *testing.T) {
	blob := test.NewBytes([]byte("the blob for our test case"))

	// set up rate limit such that we can pull this blob only twice in a row
	limit := redis_rate.Limit{Rate: len(blob.Contents) * 2, Period: time.Minute, Burst: len(blob.Contents) * 2}

	rld := basic.RateLimitDriver{
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
		h := s.Handler
		blob.MustUpload(t, s, fooRepoRef)

		// pull it via anycast
		testWithReplica(t, s, "on_first_use", func(firstPass bool, s2 test.Setup) {
			h2 := s2.Handler
			s.Clock.StepBy(time.Hour) // reset all rate limits
			testAnycast(t, firstPass, s2.DB, func() {
				anycastToken := s.GetAnycastToken(t, "repository:test1/foo:pull")
				anycastHeaders := map[string]string{
					"X-Forwarded-Host":  s.Config.AnycastAPIPublicHostname,
					"X-Forwarded-Proto": "https",
				}

				// two pulls are allowed by the rate limit (note that these are actually
				// four requests because each expectBlobExists() does one GET and one
				// HEAD, but the rate limit only counts GETs since the rate limit is on
				// the blob contents, which don't get transferred during HEAD)
				expectBlobExists(t, h2, anycastToken, "test1/foo", blob, anycastHeaders)
				expectBlobExists(t, h2, anycastToken, "test1/foo", blob, anycastHeaders)

				// third pull will be rejected by the rate limit
				assert.HTTPRequest{
					Method: "GET",
					Path:   "/v2/test1/foo/blobs/" + blob.Digest.String(),
					Header: map[string]string{
						"Authorization":     "Bearer " + anycastToken,
						"X-Forwarded-Host":  s.Config.AnycastAPIPublicHostname,
						"X-Forwarded-Proto": "https",
					},
					ExpectBody:   test.ErrorCode(keppel.ErrTooManyRequests),
					ExpectStatus: http.StatusTooManyRequests,
					ExpectHeader: map[string]string{
						test.VersionHeaderKey: test.VersionHeaderValue,
						"Retry-After":         "30",
					},
				}.Check(t, h2)

				// pull from primary is okay since we don't traverse regions
				expectBlobExists(t, h, anycastToken, "test1/foo", blob, anycastHeaders)
			})
		})
	})
}
