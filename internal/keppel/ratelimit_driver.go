// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/go-redis/redis_rate/v10"
	. "github.com/majewsky/gg/option"
	"github.com/redis/go-redis/v9"
	"github.com/sapcc/go-bits/pluggable"

	"github.com/sapcc/keppel/internal/models"
)

// RateLimitedAction is an enum of all actions that can be rate-limited.
type RateLimitedAction string

const (
	// BlobPullAction is a RateLimitedAction.
	BlobPullAction RateLimitedAction = "pullblob"
	// BlobPushAction is a RateLimitedAction.
	BlobPushAction RateLimitedAction = "pushblob"
	// ManifestPullAction is a RateLimitedAction.
	ManifestPullAction RateLimitedAction = "pullmanifest"
	// ManifestPushAction is a RateLimitedAction.
	ManifestPushAction RateLimitedAction = "pushmanifest"
	// AnycastBlobBytePullAction is a RateLimitedAction.
	// It refers to blobs being pulled from other regions via anycast.
	// The `amount` given to RateLimitAllows() shall be the blob size in bytes.
	AnycastBlobBytePullAction RateLimitedAction = "pullblobbytesanycast"
	// TrivyReportRetrieveAction is a RateLimitedAction.
	// It refers to reports being retrieved from keppel through the trivy proxy from trivy itself.
	TrivyReportRetrieveAction RateLimitedAction = "retrievetrivyreport"
)

// RateLimitDriver is a pluggable strategy that determines the rate limits of
// each account.
type RateLimitDriver interface {
	pluggable.Plugin
	// Init is called before any other interface methods, and allows the plugin to
	// perform first-time initialization.
	//
	// Implementations should inspect the auth driver to ensure that the
	// federation driver can work with this authentication method, or return
	// ErrAuthDriverMismatch otherwise.
	Init(AuthDriver, Configuration) error

	// GetRateLimit shall return None if the given action has no rate limit.
	GetRateLimit(account models.ReducedAccount, action RateLimitedAction) Option[redis_rate.Limit]
}

// RateLimitDriverRegistry is a pluggable.Registry for RateLimitDriver implementations.
var RateLimitDriverRegistry pluggable.Registry[RateLimitDriver]

// NewRateLimitDriver creates a new RateLimitDriver using one of the plugins
// registered with RateLimitDriverRegistry.
//
// The supplied config must be a string of the form {"type":"foobar","params":{...}},
// where `type` is the plugin type ID and `params` is json.Unmarshal()ed into
// the driver instance to supply driver-specific configuration.
func NewRateLimitDriver(configJSON string, ad AuthDriver, cfg Configuration) (RateLimitDriver, error) {
	callInit := func(rld RateLimitDriver) error {
		return rld.Init(ad, cfg)
	}
	return newDriver("KEPPEL_DRIVER_RATELIMIT", RateLimitDriverRegistry, configJSON, callInit)
}

////////////////////////////////////////////////////////////////////////////////

// RateLimitEngine provides the rate-limiting interface used by the API
// implementation.
type RateLimitEngine struct {
	Driver RateLimitDriver
	Client *redis.Client
}

// RateLimitAllows checks whether the given action on the given account is allowed by
// the account's rate limit.
func (e RateLimitEngine) RateLimitAllows(ctx context.Context, remoteAddr string, account models.ReducedAccount, action RateLimitedAction, amount uint64) (*redis_rate.Result, error) {
	rateQuota, ok := e.Driver.GetRateLimit(account, action).Unpack()
	if !ok {
		// no rate limit for this account and action
		return &redis_rate.Result{
			Allowed:    math.MaxInt64,
			Limit:      redis_rate.Limit{Rate: math.MaxInt64, Period: time.Second},
			Remaining:  math.MaxInt64,
			ResetAfter: 0,
			RetryAfter: -1,
		}, nil
	}

	// AllowN needs to take `amount` as an int; if this cast overflows, we fail
	// the entire ratelimit check to be safe (this should never be a problem in
	// practice because int is 64 bits wide)
	if amount > math.MaxInt {
		return &redis_rate.Result{
			Allowed:   0,
			Limit:     rateQuota,
			Remaining: 0,
			// These limits are somewhat arbitrarily chosen, but we can't have them
			// be zero because clients need to back off to a reasonable degree.
			ResetAfter: 30 * time.Second,
			RetryAfter: 30 * time.Second,
		}, nil
	}

	limiter := redis_rate.NewLimiter(e.Client)
	key := fmt.Sprintf("keppel-ratelimit-%s-%s-%s", remoteAddr, account.Name, string(action))
	result, err := limiter.AllowN(ctx, key, rateQuota, int(amount))
	if err != nil {
		return &redis_rate.Result{}, err
	}
	return result, err
}
