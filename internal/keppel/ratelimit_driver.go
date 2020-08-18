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
	"errors"
	"fmt"
	"math"

	"github.com/throttled/throttled/v2"
)

//RateLimitedAction is an enum of all actions that can be rate-limited.
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

//RateLimitDriver is a pluggable strategy that determines the rate limits of
//each account.
type RateLimitDriver interface {
	//GetRateLimit shall return nil if the given action has no rate limit.
	GetRateLimit(account Account, action RateLimitedAction) *throttled.RateQuota
}

var rateLimitDriverFactories = make(map[string]func(AuthDriver, Configuration) (RateLimitDriver, error))

//NewRateLimitDriver creates a new RateLimitDriver using one of the factory functions
//registered with RegisterRateLimitDriver().
func NewRateLimitDriver(name string, authDriver AuthDriver, cfg Configuration) (RateLimitDriver, error) {
	factory := rateLimitDriverFactories[name]
	if factory != nil {
		return factory(authDriver, cfg)
	}
	return nil, errors.New("no such rate-limit driver: " + name)
}

//RegisterRateLimitDriver registers an RateLimitDriver. Call this from func init() of the
//package defining the RateLimitDriver.
//
//Factory implementations should inspect the auth driver to ensure that the
//rate-limit driver can work with this authentication method, returning
//ErrAuthDriverMismatch otherwise.
func RegisterRateLimitDriver(name string, factory func(AuthDriver, Configuration) (RateLimitDriver, error)) {
	if _, exists := rateLimitDriverFactories[name]; exists {
		panic("attempted to register multiple rate-limit drivers with name = " + name)
	}
	rateLimitDriverFactories[name] = factory
}

////////////////////////////////////////////////////////////////////////////////

//RateLimitEngine provides the rate-limiting interface used by the API
//implementation.
type RateLimitEngine struct {
	Driver RateLimitDriver
	Store  throttled.GCRAStore
}

//RateLimitAllows checks whether the given action on the given account is allowed by
//the account's rate limit.
func (e RateLimitEngine) RateLimitAllows(account Account, action RateLimitedAction, amount uint64) (bool, throttled.RateLimitResult, error) {
	rateQuota := e.Driver.GetRateLimit(account, action)
	if rateQuota == nil {
		//no rate limit for this account and action
		return true, throttled.RateLimitResult{
			Limit:      math.MaxInt64,
			Remaining:  math.MaxInt64,
			ResetAfter: 0,
			RetryAfter: -1,
		}, nil
	}

	gcra, err := throttled.NewGCRARateLimiter(e.Store, *rateQuota)
	if err != nil {
		return false, throttled.RateLimitResult{}, err
	}
	key := fmt.Sprintf("ratelimit-%s-%s", string(action), account.Name)
	limited, result, err := gcra.RateLimit(key, int(amount))
	return !limited, result, err
}
