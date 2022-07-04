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
	"database/sql"
	"fmt"
	"time"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/keppel"
)

//query that finds the next upload to be cleaned up
var abandonedUploadSearchQuery = sqlext.SimplifyWhitespace(`
	SELECT * FROM uploads WHERE updated_at < $1
	ORDER BY updated_at ASC -- oldest uploads first
	FOR UPDATE SKIP LOCKED  -- block concurrent continuation of upload
	LIMIT 1                 -- one at a time
`)

//query that finds the account belonging to an repo object
var findAccountForRepoQuery = sqlext.SimplifyWhitespace(`
	SELECT a.* FROM accounts a
	JOIN repos r ON r.account_name = a.name
	WHERE r.id = $1
`)

//DeleteNextAbandonedUpload cleans up uploads that have not been updated for more
//than a day. At most one upload is cleaned up per call. If no upload needs to
//be cleaned up, sql.ErrNoRows is returned.
func (j *Janitor) DeleteNextAbandonedUpload() (returnErr error) {
	defer func() {
		if returnErr == nil {
			cleanupAbandonedUploadSuccessCounter.Inc()
		} else if returnErr != sql.ErrNoRows {
			cleanupAbandonedUploadFailedCounter.Inc()
			returnErr = fmt.Errorf("while deleting an abandoned upload: %s", returnErr.Error())
		}
	}()

	//we need a database transaction to be able to lock the `uploads` table row
	tx, err := j.db.Begin()
	if err != nil {
		return err
	}
	defer sqlext.RollbackUnlessCommitted(tx)

	//find upload
	var upload keppel.Upload
	maxUpdatedAt := j.timeNow().Add(-24 * time.Hour)
	err = tx.SelectOne(&upload, abandonedUploadSearchQuery, maxUpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			logg.Debug("no abandoned uploads to clean up - slowing down...")
			//explicit rollback to avoid spamming the log with "implicit rollback done" logs
			err := tx.Rollback()
			if err != nil {
				return err
			}
			return sql.ErrNoRows
		}
		return err
	}

	//find corresponding account
	var account keppel.Account
	err = tx.SelectOne(&account, findAccountForRepoQuery, upload.RepositoryID)
	if err != nil {
		return fmt.Errorf("cannot find account for abandoned upload %s: %s", upload.UUID, err.Error())
	}

	//remove from DB
	_, err = tx.Delete(&upload)
	if err != nil {
		return err
	}

	//remove from backing storage if necessary
	if upload.NumChunks > 0 {
		err := j.sd.AbortBlobUpload(account, upload.StorageID, upload.NumChunks)
		if err != nil {
			return fmt.Errorf("cannot AbortBlobUpload for abandoned upload %s: %s", upload.UUID, err.Error())
		}
	}

	return tx.Commit()
}
