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

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/throttled/throttled"
)

//RateLimitDriver is the rate limit driver "basic".
type RateLimitDriver struct {
	Limits map[keppel.RateLimitedAction]throttled.RateQuota
}

type envVarSet struct {
	RateLimit string
	Burst     string
}

var (
	envVars = map[keppel.RateLimitedAction]envVarSet{
		keppel.BlobPullAction:     {"KEPPEL_RATELIMIT_BLOB_PULLS", "KEPPEL_BURST_BLOB_PULLS"},
		keppel.BlobPushAction:     {"KEPPEL_RATELIMIT_BLOB_PUSHES", "KEPPEL_BURST_BLOB_PUSHES"},
		keppel.ManifestPullAction: {"KEPPEL_RATELIMIT_MANIFEST_PULLS", "KEPPEL_BURST_MANIFEST_PULLS"},
		keppel.ManifestPushAction: {"KEPPEL_RATELIMIT_MANIFEST_PUSHES", "KEPPEL_BURST_MANIFEST_PUSHES"},
	}
	valueRx          = regexp.MustCompile(`^\s*([0-9]+)\s*r/([smh])\s*$`)
	rateConstructors = map[string]func(int) throttled.Rate{
		"s": throttled.PerSec,
		"m": throttled.PerMin,
		"h": throttled.PerHour,
	}
)

func init() {
	keppel.RegisterRateLimitDriver("basic", func(keppel.AuthDriver, keppel.Configuration) (keppel.RateLimitDriver, error) {
		limits := make(map[keppel.RateLimitedAction]throttled.RateQuota)
		for action, envVars := range envVars {
			rate, err := parseRateLimit(envVars.RateLimit)
			if err != nil {
				return nil, err
			}
			burst, err := parseBurst(envVars.Burst)
			if err != nil {
				return nil, err
			}
			limits[action] = throttled.RateQuota{MaxRate: rate, MaxBurst: burst}
			logg.Debug("parsed rate quota for %s is %#v", action, limits[action])
		}
		return RateLimitDriver{limits}, nil
	})
}

//GetRateLimit implements the keppel.RateLimitDriver interface.
func (d RateLimitDriver) GetRateLimit(account keppel.Account, action keppel.RateLimitedAction) throttled.RateQuota {
	return d.Limits[action]
}

func parseRateLimit(envVar string) (throttled.Rate, error) {
	match := valueRx.FindStringSubmatch(keppel.MustGetenv(envVar))
	if match == nil {
		return throttled.Rate{}, fmt.Errorf("malformed %s: %q", envVar, os.Getenv(envVar))
	}
	count, err := strconv.ParseUint(match[1], 10, 32)
	if err != nil {
		return throttled.Rate{}, fmt.Errorf("malformed %s: %s", envVar, err.Error())
	}
	return rateConstructors[match[2]](int(count)), nil
}

func parseBurst(envVar string) (int, error) {
	valStr := os.Getenv(envVar)
	if valStr == "" {
		valStr = "5"
	}
	val, err := strconv.ParseUint(valStr, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("malformed %s: %s", envVar, err.Error())
	}
	return int(val), nil
}
