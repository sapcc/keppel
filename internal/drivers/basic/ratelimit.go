// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package basic

import (
	"fmt"
	"time"

	"github.com/go-redis/redis_rate/v10"
	. "github.com/majewsky/gg/option"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// RateLimitDriver is the rate limit driver "basic".
type RateLimitDriver struct {
	// raw configuration from JSON parameters
	AnycastBlobPullBytes  Option[RateLimitSpec] `json:"anycast_blob_pull_bytes"`
	BlobPulls             Option[RateLimitSpec] `json:"blob_pulls"`
	BlobPushes            Option[RateLimitSpec] `json:"blob_pushes"`
	ManifestPulls         Option[RateLimitSpec] `json:"manifest_pulls"`
	ManifestPushes        Option[RateLimitSpec] `json:"manifest_pushes"`
	TrivyReportRetrievals Option[RateLimitSpec] `json:"trivy_report_retrievals"`

	// "compiled" configuration
	Limits map[keppel.RateLimitedAction]redis_rate.Limit `json:"-"`
}

func init() {
	keppel.RateLimitDriverRegistry.Add(func() keppel.RateLimitDriver { return RateLimitDriver{} })
}

// PluginTypeID implements the keppel.RateLimitDriver interface.
func (d RateLimitDriver) PluginTypeID() string { return "basic" }

// Init implements the keppel.RateLimitDriver interface.
func (d RateLimitDriver) Init(ad keppel.AuthDriver, cfg keppel.Configuration) error {
	inputs := map[keppel.RateLimitedAction]Option[RateLimitSpec]{
		keppel.AnycastBlobBytePullAction: d.AnycastBlobPullBytes,
		keppel.BlobPullAction:            d.BlobPulls,
		keppel.BlobPushAction:            d.BlobPushes,
		keppel.ManifestPullAction:        d.ManifestPulls,
		keppel.ManifestPushAction:        d.ManifestPushes,
		keppel.TrivyReportRetrieveAction: d.TrivyReportRetrievals,
	}

	d.Limits = make(map[keppel.RateLimitedAction]redis_rate.Limit)
	for action, input := range inputs {
		spec, ok := input.Unpack()
		if !ok {
			continue
		}
		limit, err := spec.Compile()
		if err != nil {
			return fmt.Errorf("got invalid rate limit for %s: %w", action, err)
		}
		d.Limits[action] = limit
	}
	return nil
}

// GetRateLimit implements the keppel.RateLimitDriver interface.
func (d RateLimitDriver) GetRateLimit(account models.ReducedAccount, action keppel.RateLimitedAction) Option[redis_rate.Limit] {
	quota, ok := d.Limits[action]
	if ok {
		return Some(quota)
	}
	return None[redis_rate.Limit]()
}

// RateLimitSpec appears in type RateLimitDriver.
type RateLimitSpec struct {
	Limit  int    `json:"limit"`
	Window string `json:"window"`
	Burst  int    `json:"burst"`
}

var periodForWindow = map[string]time.Duration{
	"s": 1 * time.Second,
	"m": 1 * time.Minute,
	"h": 1 * time.Hour,
}

// Compile validates the config-level rate limit spec and converts it into a redis_rate.Limit object.
func (s RateLimitSpec) Compile() (redis_rate.Limit, error) {
	period, ok := periodForWindow[s.Window]
	if !ok {
		return redis_rate.Limit{}, fmt.Errorf(`invalid value for "window": %q`, s.Window)
	}
	return redis_rate.Limit{
		Rate:   s.Limit,
		Burst:  s.Burst,
		Period: period,
	}, nil
}
