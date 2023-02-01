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

package basic

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-redis/redis_rate/v10"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/osext"

	"github.com/sapcc/keppel/internal/keppel"
)

// RateLimitDriver is the rate limit driver "basic".
type RateLimitDriver struct {
	Limits map[keppel.RateLimitedAction]redis_rate.Limit
}

type envVarSet struct {
	RateLimit string
	Burst     string
}

var (
	envVars = map[keppel.RateLimitedAction]envVarSet{
		keppel.BlobPullAction:            {"KEPPEL_RATELIMIT_BLOB_PULLS", "KEPPEL_BURST_BLOB_PULLS"},
		keppel.BlobPushAction:            {"KEPPEL_RATELIMIT_BLOB_PUSHES", "KEPPEL_BURST_BLOB_PUSHES"},
		keppel.ManifestPullAction:        {"KEPPEL_RATELIMIT_MANIFEST_PULLS", "KEPPEL_BURST_MANIFEST_PULLS"},
		keppel.ManifestPushAction:        {"KEPPEL_RATELIMIT_MANIFEST_PUSHES", "KEPPEL_BURST_MANIFEST_PUSHES"},
		keppel.AnycastBlobBytePullAction: {"KEPPEL_RATELIMIT_ANYCAST_BLOB_PULL_BYTES", "KEPPEL_BURST_ANYCAST_BLOB_PULL_BYTES"},
	}
	valueRx           = regexp.MustCompile(`^\s*([0-9]+)\s*[Br]/([smh])\s*$`)
	limitConstructors = map[string]func(int) redis_rate.Limit{
		"s": redis_rate.PerSecond,
		"m": redis_rate.PerMinute,
		"h": redis_rate.PerHour,
	}
)

func init() {
	keppel.RateLimitDriverRegistry.Add(func() keppel.RateLimitDriver {
		return RateLimitDriver{make(map[keppel.RateLimitedAction]redis_rate.Limit)}
	})
}

// PluginTypeID implements the keppel.FederationDriver interface.
func (d RateLimitDriver) PluginTypeID() string { return "basic" }

// Init implements the keppel.FederationDriver interface.
func (d RateLimitDriver) Init(ad keppel.AuthDriver, cfg keppel.Configuration) error {
	for action, envVars := range envVars {
		rate, err := parseRateLimit(envVars.RateLimit)
		if err != nil {
			return err
		}
		if rate != nil {
			burst, err := parseBurst(envVars.Burst)
			if err != nil {
				return err
			}
			d.Limits[action] = redis_rate.Limit{Rate: rate.Rate, Burst: burst}
			logg.Debug("parsed rate quota for %s is %#v", action, d.Limits[action])
		}
	}
	return nil
}

// GetRateLimit implements the keppel.RateLimitDriver interface.
func (d RateLimitDriver) GetRateLimit(account keppel.Account, action keppel.RateLimitedAction) *redis_rate.Limit {
	quota, ok := d.Limits[action]
	if ok {
		return &quota
	}
	return nil
}

func parseRateLimit(envVar string) (*redis_rate.Limit, error) {
	var valStr string
	if strings.HasSuffix(envVar, "_BYTES") {
		valStr = os.Getenv(envVar)
		if valStr == "" {
			return nil, nil
		}
	} else {
		valStr = osext.MustGetenv(envVar)
	}

	match := valueRx.FindStringSubmatch(valStr)
	if match == nil {
		return nil, fmt.Errorf("malformed %s: %q", envVar, os.Getenv(envVar))
	}
	count, err := strconv.ParseUint(match[1], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("malformed %s: %s", envVar, err.Error())
	}
	rate := limitConstructors[match[2]](int(count))
	return &rate, nil
}

func parseBurst(envVar string) (int, error) {
	valStr := os.Getenv(envVar)
	if valStr == "" {
		if strings.HasSuffix(envVar, "_BYTES") {
			valStr = "0"
		} else {
			valStr = "5"
		}
	}
	val, err := strconv.Atoi(valStr)
	if err != nil {
		return 0, fmt.Errorf("malformed %s: %s", envVar, err.Error())
	}
	return val, nil
}
