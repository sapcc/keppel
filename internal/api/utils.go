// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/sapcc/go-bits/httpext"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// CheckRateLimit performs a rate limit check and renders a 429 error if the rate limit is exceeded.
func CheckRateLimit(r *http.Request, rle *keppel.RateLimitEngine, account models.ReducedAccount, authz *auth.Authorization, action keppel.RateLimitedAction, amount uint64) error {
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

	result, err := rle.RateLimitAllows(r.Context(), httpext.GetRequesterIPFor(r), account, action, amount)
	if err != nil {
		return err
	}
	if result.Allowed <= 0 {
		retryAfterStr := strconv.FormatUint(keppel.AtLeastZero(int64(result.RetryAfter/time.Second)), 10)
		return keppel.ErrTooManyRequests.With("").WithHeader("Retry-After", retryAfterStr)
	}

	return nil
}

var getTagPolicyByAccountNameQuery = sqlext.SimplifyWhitespace(`
	SELECT tag_policies_json FROM accounts WHERE name = $1
`)

// GetTagPolicies is used to read tag policies of an account.
// It is used when the initial AuthN/AuthZ check of an API call only loaded a ReducedAccount for performance reasons.
func GetTagPolicies(db *keppel.DB, account models.ReducedAccount) ([]keppel.TagPolicy, error) {
	tagPoliciesStr, err := db.SelectStr(getTagPolicyByAccountNameQuery, account.Name)
	if err != nil {
		return nil, err
	}

	return keppel.ParseTagPolicies(tagPoliciesStr)
}
