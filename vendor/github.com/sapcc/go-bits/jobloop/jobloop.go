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
	// ProcessOne finds and executes exactly one task. If no task is available to
	// be executed, `sql.ErrNoRows` is returned.
	ProcessOne() error
	// Run blocks the current goroutine and executes tasks until `ctx` expires.
	// The runtime behavior of the job can be configured through Option arguments.
	Run(ctx context.Context, opts ...Option)
}

// ProcessMany finds and executes a given amount of tasks. If not enough tasks are available to
// be executed, `sql.ErrNoRows` is returned. If any error is encountered, processing stops early.
//
// If only go would support member functions on interfaces...
func ProcessMany(j Job, count int) error {
	for i := 1; i <= count; i++ {
		err := j.ProcessOne()
		if err != nil {
			return fmt.Errorf("failed in iteration %d: %w", i, err)
		}
	}

	return nil
}
