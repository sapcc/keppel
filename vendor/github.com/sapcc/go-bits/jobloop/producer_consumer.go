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
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sapcc/go-bits/logg"
)

// ProducerConsumerJob describes a job that has one goroutine (the "producer")
// selecting tasks from an external source and one or more goroutines (the
// "consumers") executing the tasks that have been selected.
//
// Usually, the external source for the tasks is a database table, from which
// one row is selected per task. In this scenario, row-level locking should be
// used to ensure that multiple processes working on the same database table do
// not interfere with each other. If row-level locking cannot be used, the
// ConcurrencySafe field must be set to false to avoid data races.
//
// This type is parametrized over the type T (Task) which contains all data
// that is relevant to a single task, i.e. one single execution of the job.
//
// A package that implements job loops will usually provide a public API to
// spawn Job objects, and hide the task type as well as the task callbacks
// within the package, like this:
//
//	func (e *MyExecutor) EventTranslationJob(registerer prometheus.Registerer) jobloop.Job {
//	    return (&jobloop.ProducerConsumerJob[*eventTranslateTask]{ //task type is private
//	        Metadata: jobloop.JobMetadata {
//	            ReadableName:    "event translation",
//	            ConcurrencySafe: true,
//	            MetricOpts:      prometheus.CounterOpts{Name: "myservice_event_translations"},
//	            LabelNames:      []string{"event_type"},
//	        },
//	        DiscoverTask: e.findNextEventToTranslate, //function is private
//	        ProcessTask:  e.translateEvent,           //function is private
//	    }).Setup(registerer)
//	}
type ProducerConsumerJob[T any] struct {
	Metadata JobMetadata

	// A function that will be polled periodically to discover the next task
	// within this job. If there are currently no tasks waiting to be executed,
	// this function shall return `sql.ErrNoRows` to instruct the job to slow
	// down its polling.
	//
	// The provided label set will have been prefilled with the labels from
	// Metadata.CounterLabels and all label values set to "early-db-access". The
	// implementation is expected to substitute the actual label values as soon
	// as they become known.
	DiscoverTask func(prometheus.Labels) (T, error)
	// A function that will be used to process a task that has been discovered
	// within this job.
	//
	// The provided label set will have been prefilled with the labels from
	// Metadata.CounterLabels and all label values set to "early-db-access". The
	// implementation is expected to substitute the actual label values as soon
	// as they become known.
	ProcessTask func(T, prometheus.Labels) error
}

// Setup builds the Job interface for this job and registers the counter
// metric. At runtime, `nil` can be given to use the default registry. In
// tests, a test-local prometheus.Registry instance should be used instead.
func (j *ProducerConsumerJob[T]) Setup(registerer prometheus.Registerer) Job {
	if j.DiscoverTask == nil {
		panic("DiscoverTask must be set!")
	}
	if j.ProcessTask == nil {
		panic("ProcessTask must be set!")
	}

	j.Metadata.setup(registerer)
	// NOTE: We wrap `j` into a private type instead of implementing the
	// Job interface directly on `j` to enforce that callers run Setup().
	return producerConsumerJobImpl[T]{j}
}

type producerConsumerJobImpl[T any] struct {
	j *ProducerConsumerJob[T]
}

// Core producer-side behavior. This is used by ProcessOne in unit tests, as
// well as by runSingleThreaded and runMultiThreaded in production.
func (j *ProducerConsumerJob[T]) produceOne() (T, prometheus.Labels, error) {
	labels := j.Metadata.makeLabels()
	task, err := j.DiscoverTask(labels)
	if err != nil && err != sql.ErrNoRows {
		err = fmt.Errorf("could not select task for job %q: %w", j.Metadata.ReadableName, err)
		j.Metadata.countTask(labels, err)
	}
	return task, labels, err
}

// Core consumer-side behavior. This is used by ProcessOne in unit tests, as
// well as by runSingleThreaded and runMultiThreaded in production.
func (j *ProducerConsumerJob[T]) consumeOne(task T, labels prometheus.Labels) error {
	err := j.ProcessTask(task, labels)
	if err != nil {
		err = fmt.Errorf("could not process task for job %q: %w", j.Metadata.ReadableName, err)
	}
	j.Metadata.countTask(labels, err)
	return err
}

// ProcessOne implements the jobloop.Job interface.
func (i producerConsumerJobImpl[T]) ProcessOne() error {
	j := i.j

	task, labels, err := j.produceOne()
	if err != nil {
		return err
	}
	return j.consumeOne(task, labels)
}

// Run implements the jobloop.Job interface.
func (i producerConsumerJobImpl[T]) Run(ctx context.Context, opts ...Option) {
	cfg := newJobConfig(opts)

	switch cfg.NumGoroutines {
	case 0:
		panic("ProducerConsumerJob.Run() called with numGoroutines == 0")
	case 1:
		i.runSingleThreaded(ctx)
	default:
		if !i.j.Metadata.ConcurrencySafe {
			panic("ProducerConsumerJob.Run() called with numGoroutines > 1, but job is not ConcurrencySafe")
		}
		i.runMultiThreaded(ctx, cfg.NumGoroutines)
	}
}

// Implementation of Run() for `cfg.NumGoroutines == 1`.
func (i producerConsumerJobImpl[T]) runSingleThreaded(ctx context.Context) {
	for ctx.Err() == nil { //while ctx has not expired
		err := i.ProcessOne()
		logAndSlowDownOnError(err)
	}
}

type taskWithLabels[T any] struct {
	Task   T
	Labels prometheus.Labels
}

// Implementation of Run() for `cfg.NumGoroutines > 1`.
func (i producerConsumerJobImpl[T]) runMultiThreaded(ctx context.Context, numGoroutines uint) {
	j := i.j
	ch := make(chan taskWithLabels[T]) //unbuffered!
	var wg sync.WaitGroup

	//one goroutine produces tasks
	wg.Add(1)
	go func(ch chan<- taskWithLabels[T]) {
		defer wg.Done()
		for ctx.Err() == nil { //while ctx has not expired
			task, labels, err := j.produceOne()
			if err == nil {
				ch <- taskWithLabels[T]{task, labels}
			} else {
				logAndSlowDownOnError(err)
			}
		}

		//`ctx` has expired -> tell workers to shutdown
		close(ch)
	}(ch)

	//multiple goroutines consume tasks
	//
	//We use `numGoroutines-1` here since we already have spawned one goroutine
	//for the polling above.
	wg.Add(int(numGoroutines - 1))
	for i := uint(0); i < numGoroutines-1; i++ {
		go func(ch <-chan taskWithLabels[T]) {
			defer wg.Done()
			for item := range ch {
				err := j.consumeOne(item.Task, item.Labels)
				if err != nil {
					logg.Error(err.Error())
				}
			}
		}(ch)
	}

	//block until they are all done
	wg.Wait()
}

func logAndSlowDownOnError(err error) {
	switch err {
	case nil:
		//nothing to do here
	case sql.ErrNoRows:
		//no tasks waiting right now - slow down a bit to avoid useless DB load
		time.Sleep(3 * time.Second)
	default:
		//slow down a bit after an error to avoid hammering the DB during outages
		logg.Error(err.Error())
		time.Sleep(5 * time.Second)
	}
}
