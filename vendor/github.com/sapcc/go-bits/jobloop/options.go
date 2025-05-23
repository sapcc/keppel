// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package jobloop

import (
	"fmt"
	"sort"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// Option is a configuration option for a Job. Currently, only the number of
// goroutines can be configured, but more options could be added in the future.
//
// This type is an implementation of the Functional Options pattern, see e.g.
// <https://github.com/tmrts/go-patterns/blob/master/idiom/functional-options.md>
type Option func(*jobConfig)

type jobConfig struct {
	NumGoroutines   uint32
	PrefilledLabels prometheus.Labels
}

func newJobConfig(opts []Option) jobConfig {
	// default values
	cfg := jobConfig{
		NumGoroutines: 1,
	}

	// apply specific overrides
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// PrefilledLabelsAsString returns a representation of cfg.PrefilledLabels
// that is suitable for log messages.
func (cfg jobConfig) PrefilledLabelsAsString() string {
	if len(cfg.PrefilledLabels) == 0 {
		return ""
	}

	fields := make([]string, 0, len(cfg.PrefilledLabels))
	for label, value := range cfg.PrefilledLabels {
		fields = append(fields, fmt.Sprintf("%s=%q", label, value))
	}
	sort.Strings(fields)
	return fmt.Sprintf(" (%s)", strings.Join(fields, ", "))
}

// NumGoroutines is an option for a Job that allows the Job to use multiple
// goroutines, up to the specified number. The default value is 1, meaning that
// no concurrency will be employed.
//
// This option is always ignored during ProcessOne(), because a single task
// does not require concurrency on the level of the job runtime.
func NumGoroutines(n uint32) Option {
	return func(cfg *jobConfig) {
		cfg.NumGoroutines = n
	}
}

// WithLabel is an option for a Job that prefills one of the CounterLabels
// declared in the job's metadata before each task. This is useful for running
// multiple instances of a job in parallel while reusing the JobMetadata, task
// callbacks, and Prometheus metrics. Task callbacks can inspect the overridden
// label value to discover which particular instance of the job they belong to.
func WithLabel(label, value string) Option {
	return func(cfg *jobConfig) {
		if cfg.PrefilledLabels == nil {
			cfg.PrefilledLabels = make(prometheus.Labels)
		}
		cfg.PrefilledLabels[label] = value
	}
}
