// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/must"
	"go.xyrillian.de/gg/assert"

	"github.com/sapcc/keppel/internal/models"
)

func TestCleanupPendingBlobs(t *testing.T) {
	j, s := setup(t)

	tr, _ := easypg.NewTracker(t, s.DB.DB)
	cleanupJob := j.CleanupPendingBlobsJob(s.Registry)

	// right now, there are no pending blobs, so CleanupPendingBlobsJob should report nothing to do
	assert.ErrEqual(t, cleanupJob.ProcessOne(s.Ctx), nil)
	tr.DBChanges().AssertEmpty()

	// insert a pending blob into the DB
	testDigest := digest.Canonical.FromBytes([]byte("test content"))
	pendingBlob := models.PendingBlob{
		AccountName:  "test1",
		Digest:       testDigest,
		Reason:       models.PendingBecauseOfReplication,
		PendingSince: s.Clock.Now(),
		LastSeenAt:   s.Clock.Now(),
	}
	must.SucceedT(t, models.PendingBlobStore.Insert(s.Ctx, s.DB, &pendingBlob))
	tr.DBChanges().AssertEqualf(`
		INSERT INTO pending_blobs (account_name, digest, reason, since, last_seen_at) VALUES ('test1', '%s', 'replication', %d, %d);
	`, testDigest, s.Clock.Now().Unix(), s.Clock.Now().Unix(),
	)

	// the pending blob's heartbeat is too recent (less than 1 minute old), so it should not be cleaned up
	s.Clock.StepBy(30 * time.Second)
	assert.ErrEqual(t, cleanupJob.ProcessOne(s.Ctx), nil)
	tr.DBChanges().AssertEmpty()

	// after 1 minute has passed since the last heartbeat, the pending blob should be cleaned up
	s.Clock.StepBy(45 * time.Second)
	assert.ErrEqual(t, cleanupJob.ProcessOne(s.Ctx), nil)
	tr.DBChanges().AssertEqualf(`
		DELETE FROM pending_blobs WHERE account_name = 'test1' AND digest = '%s';
	`, testDigest,
	)
}
