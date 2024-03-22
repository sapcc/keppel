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
	"errors"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sapcc/go-bits/sqlext"
)

// TxGuardedJob is a specialization of ProducerConsumerJob, where each task
// corresponds to one row of a database table that needs to be worked on.
// Rows must be selected in a ConcurrencySafe way (most commonly through the
// "SELECT ... FOR UPDATE SKIP LOCKED" mechanism). The job implementation
// ensures that the entirety of each task runs within a single SQL transaction,
// and manages the lifecycle of that transaction.
//
// This type works in the same way as ProducerConsumerJob, except that it
// offers a different set of callbacks. The type arguments are:
//   - P (Payload), the payload for one individual task (e.g. the ORM object
//     corresponding to the selected row)
//   - Tx (Transaction), the type for a DB transaction (the job implementation
//     will call Rollback on this in case of errors)
//
// Just like ProducerConsumerJob, the type arguments are often private types,
// and the job type as well as the callbacks are hidden within the defining
// package, like this:
//
//	func (e *MyExecutor) EventTranslationJob(registerer prometheus.Registerer) jobloop.Job {
//	    return (&jobloop.TxGuardedJob[*sql.Tx, dbmodel.Event]{
//	        Metadata: jobloop.JobMetadata {
//	            ReadableName:    "event translation",
//	            ConcurrencySafe: true,
//	            MetricOpts:      prometheus.CounterOpts{Name: "myservice_event_translations"},
//	            LabelNames:      []string{"event_type"},
//	        },
//	        BeginTx:     e.DB.Begin,
//	        DiscoverRow: e.findNextEventToTranslate, //function is private
//	        ProcessRow:  e.translateEvent,           //function is private
//	    }).Setup(registerer)
type TxGuardedJob[Tx sqlext.Rollbacker, P any] struct {
	Metadata JobMetadata

	// A function that begins a new DB transaction. Usually set to `db.Begin`.
	BeginTx func() (Tx, error)
	// A function that will be polled periodically (once per transaction) to
	// discover the next row to work on. If there are currently no rows waiting
	// to be processed, this function shall return `sql.ErrNoRows` to instruct
	// the job to slow down its polling.
	//
	// The provided label set will have been prefilled with the labels from
	// Metadata.CounterLabels and all label values set to "early-db-access". The
	// implementation is expected to substitute the actual label values as soon
	// as they become known.
	DiscoverRow func(context.Context, Tx, prometheus.Labels) (P, error)
	// A function that will be called once for each discovered row to process it.
	//
	// The provided label set will have been prefilled with the labels from
	// Metadata.CounterLabels and all label values set to "early-db-access". The
	// implementation is expected to substitute the actual label values as soon
	// as they become known.
	ProcessRow func(context.Context, Tx, P, prometheus.Labels) error
}

// Setup builds the Job interface for this job and registers the counter
// metric. At runtime, `nil` can be given to use the default registry. In
// tests, a test-local prometheus.Registry instance should be used instead.
func (j *TxGuardedJob[Tx, P]) Setup(registerer prometheus.Registerer) Job {
	if j.BeginTx == nil {
		panic("BeginTx must be set!")
	}
	if j.DiscoverRow == nil {
		panic("DiscoverRow must be set!")
	}
	if j.ProcessRow == nil {
		panic("ProcessRow must be set!")
	}

	return (&ProducerConsumerJob[*txGuardedTask[Tx, P]]{
		Metadata:     j.Metadata,
		DiscoverTask: j.discoverTask,
		ProcessTask:  j.processTask,
	}).Setup(registerer)
}

type txGuardedTask[Tx sqlext.Rollbacker, P any] struct {
	Transaction Tx
	Payload     P
}

// Core producer-side behavior. This is used by ProcessOne in unit tests, as
// well as by runSingleThreaded and runMultiThreaded in production.
func (j *TxGuardedJob[Tx, P]) discoverTask(ctx context.Context, labels prometheus.Labels) (task *txGuardedTask[Tx, P], returnedError error) {
	tx, err := j.BeginTx()
	if err != nil {
		return nil, err
	}
	defer func() {
		if returnedError != nil {
			sqlext.RollbackUnlessCommitted(tx)
		}
	}()

	payload, err := j.DiscoverRow(ctx, tx, labels)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			//nolint:errcheck
			tx.Rollback() // avoid the log line generated by sqlext.RollbackUnlessCommitted()
		}
		return nil, err
	}

	return &txGuardedTask[Tx, P]{
		Transaction: tx,
		Payload:     payload,
	}, nil
}

func (j *TxGuardedJob[Tx, P]) processTask(ctx context.Context, task *txGuardedTask[Tx, P], labels prometheus.Labels) error {
	defer sqlext.RollbackUnlessCommitted(task.Transaction)
	return j.ProcessRow(ctx, task.Transaction, task.Payload, labels)
}
