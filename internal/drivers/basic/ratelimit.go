// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

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
	"github.com/sapcc/keppel/internal/models"
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
		keppel.TrivyReportRetrieveAction: {"KEPPEL_RATELIMIT_TRIVY_REPORT_RETRIEVALS", "KEPPEL_BURST_TRIVY_REPORT_RETRIEVALS"},
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

// PluginTypeID implements the keppel.RateLimitDriver interface.
func (d RateLimitDriver) PluginTypeID() string { return "basic" }

// Init implements the keppel.RateLimitDriver interface.
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
			d.Limits[action] = redis_rate.Limit{Rate: rate.Rate, Burst: burst, Period: rate.Period}
			logg.Debug("parsed rate quota for %s is %#v", action, d.Limits[action])
		}
	}
	return nil
}

// GetRateLimit implements the keppel.RateLimitDriver interface.
func (d RateLimitDriver) GetRateLimit(account models.ReducedAccount, action keppel.RateLimitedAction) *redis_rate.Limit {
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
	count, err := strconv.Atoi(match[1])
	if err != nil {
		return nil, fmt.Errorf("malformed %s: %s", envVar, err.Error())
	}
	rate := limitConstructors[match[2]](count)
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
