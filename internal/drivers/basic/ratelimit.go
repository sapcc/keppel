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

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/throttled/throttled"
)

//RateLimitDriver is the rate limit driver "basic".
type RateLimitDriver struct {
	Limits map[keppel.RateLimitedAction]throttled.Rate
}

var (
	envVars = map[string]keppel.RateLimitedAction{
		"KEPPEL_RATELIMIT_BLOB_PULLS":      keppel.BlobPullAction,
		"KEPPEL_RATELIMIT_BLOB_PUSHES":     keppel.BlobPushAction,
		"KEPPEL_RATELIMIT_MANIFEST_PULLS":  keppel.ManifestPullAction,
		"KEPPEL_RATELIMIT_MANIFEST_PUSHES": keppel.ManifestPushAction,
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
		limits := make(map[keppel.RateLimitedAction]throttled.Rate)
		for envVar, action := range envVars {
			match := valueRx.FindStringSubmatch(keppel.MustGetenv(envVar))
			if match == nil {
				return nil, fmt.Errorf("malformed %s: %q", envVar, os.Getenv(envVar))
			}
			count, err := strconv.ParseUint(match[1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("malformed %s: %s", envVar, err.Error())
			}
			limits[action] = rateConstructors[match[2]](int(count))
		}
		return RateLimitDriver{limits}, nil
	})
}

//GetRateLimit implements the keppel.RateLimitDriver interface.
func (d RateLimitDriver) GetRateLimit(account keppel.Account, action keppel.RateLimitedAction) throttled.Rate {
	return d.Limits[action]
}
