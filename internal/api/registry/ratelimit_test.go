/******************************************************************************
*
*  Copyright 2020 SAP SE
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

package registryv2

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/keppel/internal/drivers/basic"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
	"github.com/throttled/throttled/v2"
	"github.com/throttled/throttled/v2/store/memstore"
)

func TestRateLimits(t *testing.T) {
	rateQuota := throttled.RateQuota{MaxRate: throttled.PerMin(2), MaxBurst: 3}
	rld := basic.RateLimitDriver{
		Limits: map[keppel.RateLimitedAction]throttled.RateQuota{
			keppel.BlobPullAction:     rateQuota,
			keppel.BlobPushAction:     rateQuota,
			keppel.ManifestPullAction: rateQuota,
			keppel.ManifestPushAction: rateQuota,
		},
	}
	rle := &keppel.RateLimitEngine{Driver: rld, Store: nil}

	testWithPrimary(t, rle, func(h http.Handler, cfg keppel.Configuration, db *keppel.DB, ad *test.AuthDriver, sd *test.StorageDriver, fd *test.FederationDriver, clock *test.Clock) {
		rls, err := memstore.New(-1)
		if err != nil {
			t.Fatal(err.Error())
		}
		rls.SetTimeNow(clock.Now)
		rle.Store = rls

		//create the "test1/foo" repository to ensure that we don't just always hit
		//NAME_UNKNOWN errors
		_, err = keppel.FindOrCreateRepository(db, "foo", keppel.Account{Name: "test1"})
		if err != nil {
			t.Fatal(err.Error())
		}

		token := getToken(t, h, ad, "repository:test1/foo:pull,push",
			keppel.CanPullFromAccount,
			keppel.CanPushToAccount)
		bogusDigest := "sha256:" + sha256Of([]byte("something else"))

		//prepare some test requests that should be affected by rate limiting
		//(some of these fail with 404 or 400, but that's okay; the important part is
		//whether they fail with 429 or not)
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
			clock.StepBy(time.Hour)

			//we can always execute 1 request initially, and then we can burst on top
			//of that
			for i := 0; i < rateQuota.MaxBurst+1; i++ {
				req.Check(t, h)
				clock.StepBy(time.Second)
			}

			//then the next request should be rate-limited
			failingReq := req
			failingReq.ExpectBody = test.ErrorCode(keppel.ErrTooManyRequests)
			failingReq.ExpectStatus = http.StatusTooManyRequests
			failingReq.ExpectHeader = map[string]string{
				test.VersionHeaderKey: test.VersionHeaderValue,
				"Retry-After":         strconv.Itoa(29 - rateQuota.MaxBurst),
			}
			failingReq.Check(t, h)

			//be impatient
			clock.StepBy(time.Duration(28-rateQuota.MaxBurst) * time.Second)
			failingReq.ExpectHeader["Retry-After"] = "1"
			failingReq.Check(t, h)

			//finally!
			clock.StepBy(time.Second)
			req.Check(t, h)

			//aaaand... we're rate-limited again immediately because we haven't
			//recovered our burst budget yet
			failingReq.ExpectHeader["Retry-After"] = "30"
			failingReq.Check(t, h)
		}
	})
}

func TestAnycastRateLimits(t *testing.T) {
	blob := test.NewBytes([]byte("the blob for our test case"))

	//set up rate limit such that we can pull this blob only twice in a row
	rateQuota := throttled.RateQuota{MaxRate: throttled.PerMin(len(blob.Contents) * 2), MaxBurst: len(blob.Contents) * 2}
	rld := basic.RateLimitDriver{
		Limits: map[keppel.RateLimitedAction]throttled.RateQuota{
			keppel.AnycastBlobBytePullAction: rateQuota,
			//all other rate limits are set to "unlimited"
		},
	}
	rle := &keppel.RateLimitEngine{Driver: rld, Store: nil}

	testWithPrimary(t, rle, func(h http.Handler, cfg keppel.Configuration, db *keppel.DB, ad *test.AuthDriver, sd *test.StorageDriver, fd *test.FederationDriver, clock *test.Clock) {
		if !currentScenario.WithAnycast {
			return
		}
		rls, err := memstore.New(-1)
		if err != nil {
			t.Fatal(err.Error())
		}
		rls.SetTimeNow(clock.Now)
		rle.Store = rls

		//upload the test blob
		uploadToken := getToken(t, h, ad, "repository:test1/foo:pull,push",
			keppel.CanPullFromAccount,
			keppel.CanPushToAccount)
		uploadBlob(t, h, uploadToken, "test1/foo", blob)

		//pull it via anycast
		testWithReplica(t, h, db, clock, "on_first_use", func(firstPass bool, h2 http.Handler, cfg2 keppel.Configuration, db2 *keppel.DB, ad2 *test.AuthDriver, sd2 *test.StorageDriver) {
			clock.StepBy(time.Hour) //reset all rate limits
			testAnycast(t, firstPass, db2, func() {

				anycastToken := getTokenForAnycast(t, h, ad, "repository:test1/foo:pull",
					keppel.CanPullFromAccount)
				anycastHeaders := map[string]string{
					"X-Forwarded-Host":  cfg.AnycastAPIPublicURL.Hostname(),
					"X-Forwarded-Proto": "https",
				}

				//two pulls are allowed by the rate limit (note that these are actually
				//four requests because each expectBlobExists() does one GET and one
				//HEAD, but the rate limit only counts GETs since the rate limit is on
				//the blob contents, which don't get transferred during HEAD)
				expectBlobExists(t, h2, anycastToken, "test1/foo", blob, anycastHeaders)
				expectBlobExists(t, h2, anycastToken, "test1/foo", blob, anycastHeaders)

				//third pull will be rejected by the rate limit
				assert.HTTPRequest{
					Method: "GET",
					Path:   "/v2/test1/foo/blobs/" + blob.Digest.String(),
					Header: map[string]string{
						"Authorization":     "Bearer " + anycastToken,
						"X-Forwarded-Host":  cfg.AnycastAPIPublicURL.Hostname(),
						"X-Forwarded-Proto": "https",
					},
					ExpectBody:   test.ErrorCode(keppel.ErrTooManyRequests),
					ExpectStatus: http.StatusTooManyRequests,
					ExpectHeader: map[string]string{
						test.VersionHeaderKey: test.VersionHeaderValue,
						"Retry-After":         "28",
					},
				}.Check(t, h2)

				//pull from primary is okay since we don't traverse regions
				expectBlobExists(t, h, anycastToken, "test1/foo", blob, anycastHeaders)
			})
		})
	})
}
