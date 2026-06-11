// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/jobloop"
	"github.com/sapcc/go-bits/sqlext"
	"go.xyrillian.de/oblast"

	"github.com/sapcc/keppel/internal/models"
)

// query that finds the next upload to be cleaned up
var abandonedUploadSearchQuery = sqlext.SimplifyWhitespace(`
	SELECT * FROM uploads WHERE updated_at < $1
	ORDER BY updated_at ASC -- oldest uploads first
	FOR UPDATE SKIP LOCKED  -- block concurrent continuation of upload
	LIMIT 1                 -- one at a time
`)

// query that finds the account belonging to an repo object
var findAccountForRepoIDQuery = models.AccountStore.MustPrepareSelectQueryWhere(`name = (SELECT account_name FROM repos WHERE id = $1)`)

// AbandonedUploadCleanupJob is a jobloop.Job. Each task finds an upload that has not
// been updated for more than a day, and cleans it up.
func (j *Janitor) AbandonedUploadCleanupJob(registerer prometheus.Registerer) jobloop.Job {
	return (&jobloop.TxGuardedJob[*oblast.Tx, models.Upload]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "cleanup of abandoned uploads",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_abandoned_upload_cleanups",
				Help: "Counter for cleanup operations for abandoned uploads.",
			},
		},
		BeginTx: j.db.Begin,
		DiscoverRow: func(ctx context.Context, tx *oblast.Tx, _ prometheus.Labels) (models.Upload, error) {
			maxUpdatedAt := j.timeNow().Add(-24 * time.Hour)
			return models.UploadStore.SelectOne(ctx, tx, abandonedUploadSearchQuery, maxUpdatedAt)
		},
		ProcessRow: j.deleteAbandonedUpload,
	}).Setup(registerer)
}

func (j *Janitor) deleteAbandonedUpload(ctx context.Context, tx *oblast.Tx, upload models.Upload, labels prometheus.Labels) error {
	// find corresponding account
	account, err := findAccountForRepoIDQuery.SelectOne(ctx, tx, upload.RepositoryID)
	if err != nil {
		return fmt.Errorf("cannot find account for abandoned upload %s: %s", upload.UUID, err.Error())
	}

	// remove from DB
	err = models.UploadStore.Delete(ctx, tx, upload)
	if err != nil {
		return err
	}

	// remove from backing storage if necessary
	if upload.NumChunks > 0 {
		err := j.sd.AbortBlobUpload(ctx, account.Reduced(), upload.StorageID, upload.NumChunks)
		if err != nil {
			return fmt.Errorf("cannot AbortBlobUpload for abandoned upload %s: %s", upload.UUID, err.Error())
		}
	}

	return tx.Commit()
}
