// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/jobloop"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/models"
)

var stalePendingBlobsQuery = sqlext.SimplifyWhitespace(`
	SELECT * FROM pending_blobs
	WHERE since < $1
	ORDER BY since ASC
	FOR UPDATE SKIP LOCKED
	LIMIT 1
`)

// CleanupPendingBlobsJob is a jobloop.Job. Each task finds one pending_blob that is not locked and at least 5 minutes old and deletes it.
func (j *Janitor) CleanupPendingBlobsJob(registerer prometheus.Registerer) jobloop.Job { //nolint:dupl // false positive
	return (&jobloop.ProducerConsumerJob[models.PendingBlob]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "cleanup pending blobs",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_cleanup_pending_blobs",
				Help: "Counter for garbage collections on orphaned pending blobs.",
			},
		},
		DiscoverTask: func(ctx context.Context, _ prometheus.Labels) (models.PendingBlob, error) {
			maxPendingSince := j.timeNow().Add(-5 * time.Minute)
			return models.PendingBlobStore.SelectOne(ctx, j.db, stalePendingBlobsQuery, maxPendingSince)
		},
		ProcessTask: j.sweepPendingBlobs,
	}).Setup(registerer)
}

func (j *Janitor) sweepPendingBlobs(ctx context.Context, pendingBlob models.PendingBlob, _ prometheus.Labels) error {
	logg.Info("cleaning up stale pending blob %s in account %s", pendingBlob.Digest, pendingBlob.AccountName)
	return models.PendingBlobStore.Delete(ctx, j.db, pendingBlob)
}
