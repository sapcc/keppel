/******************************************************************************
*
*  Copyright 2020 SAP SE
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

package tasks

import (
	"context"
	"fmt"
	"time"

	"github.com/go-gorp/gorp/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/jobloop"
	"github.com/sapcc/go-bits/sqlext"

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
var findAccountForRepoQuery = sqlext.SimplifyWhitespace(`
	SELECT a.* FROM accounts a
	JOIN repos r ON r.account_name = a.name
	WHERE r.id = $1
`)

// AbandonedUploadCleanupJob is a job. Each task finds an upload that has not
// been updated for more than a day, and cleans it up.
func (j *Janitor) AbandonedUploadCleanupJob(registerer prometheus.Registerer) jobloop.Job {
	return (&jobloop.TxGuardedJob[*gorp.Transaction, models.Upload]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "cleanup of abandoned uploads",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_abandoned_upload_cleanups",
				Help: "Counter for cleanup operations for abandoned uploads.",
			},
		},
		BeginTx: j.db.Begin,
		DiscoverRow: func(_ context.Context, tx *gorp.Transaction, _ prometheus.Labels) (upload models.Upload, err error) {
			maxUpdatedAt := j.timeNow().Add(-24 * time.Hour)
			err = tx.SelectOne(&upload, abandonedUploadSearchQuery, maxUpdatedAt)
			return upload, err
		},
		ProcessRow: j.deleteAbandonedUpload,
	}).Setup(registerer)
}

func (j *Janitor) deleteAbandonedUpload(ctx context.Context, tx *gorp.Transaction, upload models.Upload, labels prometheus.Labels) error {
	// find corresponding account
	var account models.Account
	err := tx.SelectOne(&account, findAccountForRepoQuery, upload.RepositoryID)
	if err != nil {
		return fmt.Errorf("cannot find account for abandoned upload %s: %s", upload.UUID, err.Error())
	}

	// remove from DB
	_, err = tx.Delete(&upload)
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
