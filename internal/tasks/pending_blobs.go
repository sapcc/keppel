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

var deleteStalePendingBlobsQuery = sqlext.SimplifyWhitespace(`
	DELETE FROM pending_blobs
	WHERE last_seen_at < $1
	RETURNING *
`)

// CleanupPendingBlobsJob is a jobloop.Job. Each task deletes all pending_blobs whose heartbeat is older than 1 minute.
func (j *Janitor) CleanupPendingBlobsJob(registerer prometheus.Registerer) jobloop.Job {
	return (&jobloop.CronJob{
		Metadata: jobloop.JobMetadata{
			ReadableName: "cleanup pending blobs",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_cleanup_pending_blobs",
				Help: "Counter for garbage collections on orphaned pending blobs.",
			},
		},
		Interval: 1 * time.Minute,
		Task:     j.cleanupPendingBlobs,
	}).Setup(registerer)
}

func (j *Janitor) cleanupPendingBlobs(ctx context.Context, _ prometheus.Labels) error {
	maxLastSeenAt := j.timeNow().Add(-1 * time.Minute)
	return models.PendingBlobStore.Select(ctx, j.db, deleteStalePendingBlobsQuery, maxLastSeenAt).Foreach(func(pendingBlob models.PendingBlob) error {
		logg.Info("cleaned up stale pending blob %s in account %s", pendingBlob.Digest, pendingBlob.AccountName)
		return nil
	})
}
