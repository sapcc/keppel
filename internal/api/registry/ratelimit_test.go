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
	rls := newTestGCRAStore()
	rle := &keppel.RateLimitEngine{Driver: rld, Store: rls}

	h, _, db, ad, _, clock := setup(t, rle)
	rls.TimeNow = clock.Now

	//create the "test1/foo" repository to ensure that we don't just always hit
	//NAME_UNKNOWN errors
	_, err := keppel.FindOrCreateRepository(db, "foo", keppel.Account{Name: "test1"})
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
		//TODO more
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
}

////////////////////////////////////////////////////////////////////////////////
// type testGCRAStore

//testGCRAStore is a throttled.GCRAStore with in-memory storage that can be
//connected to test.Clock. The implementation is not thread-safe, which is not
//a problem since our tests don't run concurrently.
type testGCRAStore struct {
	Values  map[string]int64
	TimeNow func() time.Time
}

func newTestGCRAStore() *testGCRAStore {
	return &testGCRAStore{make(map[string]int64), nil}
}

func (s *testGCRAStore) GetWithTime(key string) (int64, time.Time, error) {
	val, ok := s.Values[key]
	if !ok {
		val = -1
	}
	return val, s.TimeNow(), nil
}

func (s *testGCRAStore) SetIfNotExistsWithTTL(key string, value int64, _ time.Duration) (bool, error) {
	_, ok := s.Values[key]
	if ok {
		return false, nil
	}
	s.Values[key] = value
	return true, nil
}

func (s *testGCRAStore) CompareAndSwapWithTTL(key string, oldValue, newValue int64, _ time.Duration) (bool, error) {
	val, ok := s.Values[key]
	if !ok {
		return false, nil
	}
	if val == oldValue {
		s.Values[key] = newValue
		return true, nil
	}
	return false, nil
}
