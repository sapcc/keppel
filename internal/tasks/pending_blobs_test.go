// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"database/sql"
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
	assert.ErrEqual(t, cleanupJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().AssertEmpty()

	// insert a pending blob into the DB
	testDigest := digest.Canonical.FromBytes([]byte("test content"))
	pendingBlob := models.PendingBlob{
		AccountName:  "test1",
		Digest:       testDigest,
		Reason:       models.PendingBecauseOfReplication,
		PendingSince: s.Clock.Now(),
	}
	must.SucceedT(t, models.PendingBlobStore.Insert(s.Ctx, s.DB, &pendingBlob))
	tr.DBChanges().AssertEqualf(`
		INSERT INTO pending_blobs (account_name, digest, reason, since) VALUES ('test1', '%s', 'replication', %d);
	`, testDigest, s.Clock.Now().Unix(),
	)

	// the pending blob is too recent (less than 5 minutes old), so it should not be cleaned up
	s.Clock.StepBy(3 * time.Minute)
	assert.ErrEqual(t, cleanupJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().AssertEmpty()

	// after 5 minutes have passed, the pending blob should be cleaned up
	s.Clock.StepBy(3 * time.Minute)
	assert.ErrEqual(t, cleanupJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, cleanupJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().AssertEqualf(`
		DELETE FROM pending_blobs WHERE account_name = 'test1' AND digest = '%s';
	`, testDigest,
	)
}
