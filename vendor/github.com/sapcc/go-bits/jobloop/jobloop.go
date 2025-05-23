// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

// Package jobloop contains the Job trait that abstracts over several types of
// worker loops. The package provides the basic implementation of these worker
// loops, including basic instrumentation.
package jobloop

import (
	"context"
	"fmt"
)

// TODO upstream this package into go-bits once proven in here

// Job describes a loop that executes instances of a specific type of task.
type Job interface {
	// ProcessOne finds and executes exactly one task, aborting early if `ctx` expires.
	// If no task is available to be executed, `sql.ErrNoRows` is returned.
	// The runtime behavior of the job can be configured through Option arguments.
	ProcessOne(ctx context.Context, opts ...Option) error
	// Run blocks the current goroutine and executes tasks until `ctx` expires.
	// The runtime behavior of the job can be configured through Option arguments.
	Run(ctx context.Context, opts ...Option)
}

// ProcessMany finds and executes a given amount of tasks. If not enough tasks are available to
// be executed, `sql.ErrNoRows` is returned. If any error is encountered, processing stops early.
//
// If only go would support member functions on interfaces...
func ProcessMany(j Job, ctx context.Context, count int, opts ...Option) error {
	for i := 1; i <= count; i++ {
		err := j.ProcessOne(ctx, opts...)
		if err != nil {
			return fmt.Errorf("failed in iteration %d: %w", i, err)
		}
	}

	return nil
}
