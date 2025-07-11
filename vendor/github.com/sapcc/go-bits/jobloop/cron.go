// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package jobloop

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sapcc/go-bits/logg"
)

// CronJob is a job loop that executes in a set interval.
type CronJob struct {
	Metadata JobMetadata
	Interval time.Duration

	// By default, the job will wait out a full Interval before running for the first time.
	// If an earlier first run is desired, InitialDelay can be set to a non-zero value that is smaller than Interval.
	InitialDelay time.Duration

	// A function that will be executed by this job once per Interval.
	//
	// The provided label set will have been prefilled with the labels from
	// Metadata.CounterLabels and all label values set to "early-db-access".
	// The implementation is expected to substitute the actual label values as
	// soon as they become known.
	Task func(context.Context, prometheus.Labels) error
}

// Setup builds the Job interface for this job and registers the counter
// metric. At runtime, `nil` can be given to use the default registry. In
// tests, a test-local prometheus.Registry instance should be used instead.
func (j *CronJob) Setup(registerer prometheus.Registerer) Job {
	if j.Task == nil {
		panic("Task must be set!")
	}

	j.Metadata.setup(registerer)
	// NOTE: We wrap `j` into a private type instead of implementing the
	// Job interface directly on `j` to enforce that callers run Setup().
	return cronJobImpl{j}
}

type cronJobImpl struct {
	j *CronJob
}

// Core behavior of ProcessOne(). This is a separate function because it is reused in runOnce().
func (i cronJobImpl) processOne(ctx context.Context, cfg jobConfig) error {
	j := i.j

	labels := j.Metadata.makeLabels(cfg)
	err := j.Task(ctx, labels)
	j.Metadata.countTask(labels, err)
	return j.Metadata.enrichError("run", err, cfg)
}

// ProcessOne implements the Job interface.
func (i cronJobImpl) ProcessOne(ctx context.Context, opts ...Option) error {
	cfg := newJobConfig(opts)
	// ProcessOne() is usually called during tests, so adding extra context to error messages is not helpful
	// (it would only make error message matches more convoluted)
	cfg.WantsExtraErrorContext = false

	return i.processOne(ctx, cfg)
}

// Run implements the Job interface.
func (i cronJobImpl) Run(ctx context.Context, opts ...Option) {
	cfg := newJobConfig(opts)
	cfg.WantsExtraErrorContext = true
	runOnce := func() {
		err := i.processOne(ctx, cfg)
		if err != nil {
			logg.Error(err.Error())
		}
	}

	if i.j.InitialDelay != 0 {
		time.Sleep(i.j.InitialDelay)
		runOnce()
	}

	ticker := time.NewTicker(i.j.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce()
		}
	}
}
