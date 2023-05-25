/******************************************************************************
*
*  Copyright 2023 SAP SE
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

// ProcessOne implements the Job interface.
func (i cronJobImpl) ProcessOne(ctx context.Context) error {
	j := i.j

	labels := j.Metadata.makeLabels()
	err := j.Task(ctx, labels)
	j.Metadata.countTask(labels, err)
	return err
}

// Run implements the Job interface.
func (i cronJobImpl) Run(ctx context.Context, opts ...Option) {
	ticker := time.NewTicker(i.j.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := i.ProcessOne(ctx)
			if err != nil {
				logg.Error("could not run task for job %q: %s", i.j.Metadata.ReadableName, err.Error())
			}
		}
	}
}
