/******************************************************************************
*
*  Copyright 2023 SAP SE
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

package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/sapcc/go-bits/httpext"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

func CheckRateLimit(r *http.Request, rle *keppel.RateLimitEngine, account models.Account, authz *auth.Authorization, action keppel.RateLimitedAction, amount uint64) error {
	// rate-limiting is optional
	if rle == nil {
		return nil
	}

	// cluster-internal traffic is exempt from rate-limits (if the request is
	// caused by a user API request, the rate-limit has been checked already
	// before the cluster-internal request was sent)
	userType := authz.UserIdentity.UserType()
	if userType == keppel.PeerUser || userType == keppel.TrivyUser {
		return nil
	}

	allowed, result, err := rle.RateLimitAllows(r.Context(), httpext.GetRequesterIPFor(r), account, action, amount)
	if err != nil {
		return err
	}
	if !allowed {
		retryAfterStr := strconv.FormatUint(keppel.AtLeastZero(int64(result.RetryAfter/time.Second)), 10)
		return keppel.ErrTooManyRequests.With("").WithHeader("Retry-After", retryAfterStr)
	}

	return nil
}
