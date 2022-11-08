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

package keppel

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/go-redis/redis_rate/v9"
	"github.com/sapcc/go-bits/pluggable"
)

// RateLimitedAction is an enum of all actions that can be rate-limited.
type RateLimitedAction string

const (
	//BlobPullAction is a RateLimitedAction.
	BlobPullAction RateLimitedAction = "pullblob"
	//BlobPushAction is a RateLimitedAction.
	BlobPushAction RateLimitedAction = "pushblob"
	//ManifestPullAction is a RateLimitedAction.
	ManifestPullAction RateLimitedAction = "pullmanifest"
	//ManifestPushAction is a RateLimitedAction.
	ManifestPushAction RateLimitedAction = "pushmanifest"
	//AnycastBlobBytePullAction is a RateLimitedAction. It refers to blobs being
	//pulled from other regions via anycast. The `amount` given to
	//RateLimitAllows() shall be the blob size in bytes.
	AnycastBlobBytePullAction RateLimitedAction = "pullblobbytesanycast"
)

// RateLimitDriver is a pluggable strategy that determines the rate limits of
// each account.
type RateLimitDriver interface {
	pluggable.Plugin
	//Init is called before any other interface methods, and allows the plugin to
	//perform first-time initialization.
	//
	//Implementations should inspect the auth driver to ensure that the
	//federation driver can work with this authentication method, or return
	//ErrAuthDriverMismatch otherwise.
	Init(AuthDriver, Configuration) error

	//GetRateLimit shall return nil if the given action has no rate limit.
	GetRateLimit(account Account, action RateLimitedAction) *redis_rate.Limit
}

// RateLimitDriverRegistry is a pluggable.Registry for RateLimitDriver implementations.
var RateLimitDriverRegistry pluggable.Registry[RateLimitDriver]

// NewRateLimitDriver creates a new RateLimitDriver using one of the plugins
// registered with RateLimitDriverRegistry.
func NewRateLimitDriver(pluginTypeID string, ad AuthDriver, cfg Configuration) (RateLimitDriver, error) {
	rld := RateLimitDriverRegistry.Instantiate(pluginTypeID)
	if rld == nil {
		return nil, errors.New("no such rate-limit driver: " + pluginTypeID)
	}
	return rld, rld.Init(ad, cfg)
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
func (e RateLimitEngine) RateLimitAllows(account Account, action RateLimitedAction, amount uint64) (bool, *redis_rate.Result, error) {
	rateQuota := e.Driver.GetRateLimit(account, action)
	if rateQuota == nil {
		//no rate limit for this account and action
		return true, &redis_rate.Result{
			Limit:      redis_rate.Limit{Rate: math.MaxInt64, Period: time.Second},
			Remaining:  math.MaxInt64,
			ResetAfter: 0,
			RetryAfter: -1,
		}, nil
	}

	limiter := redis_rate.NewLimiter(e.Client)
	key := fmt.Sprintf("keppel-ratelimit-%s-%s", string(action), account.Name)
	result, err := limiter.AllowN(context.Background(), key, *rateQuota, int(amount))
	if err != nil {
		return false, &redis_rate.Result{}, err
	}
	return result.Allowed > 0, result, err
}
