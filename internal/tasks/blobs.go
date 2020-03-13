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
	"github.com/sapcc/keppel/internal/keppel"
)

const blobSweepSearchQuery = `
	SELECT * FROM accounts
		WHERE blobs_sweeped_at IS NULL OR blobs_sweeped_at < $1
	-- accounts without any sweeps first, then sorted by last sweep
	ORDER BY blobs_sweeped_at IS NULL DESC, blobs_sweeped_at ASC
	-- only one account at a time
	LIMIT 1
`

const blobMarkQuery = `
	UPDATE blobs SET marked_for_deletion_at = $2
	WHERE account_name = $1 AND marked_for_deletion_at IS NULL AND id NOT IN (
		SELECT m.blob_id FROM blob_mounts m JOIN repos r ON m.repo_id = r.id
		WHERE r.account_name = $1
	)
`

const blobUnmarkQuery = `
	UPDATE blobs SET marked_for_deletion_at = NULL
	WHERE account_name = $1 AND id IN (
		SELECT m.blob_id FROM blob_mounts m JOIN repos r ON m.repo_id = r.id
		WHERE r.account_name = $1
	)
`

const blobSelectMarkedQuery = `
	SELECT * FROM blobs WHERE account_name = $1 AND marked_for_deletion_at < $2
`

const blobSweepDoneQuery = `
	UPDATE accounts SET blobs_sweeped_at = $2 WHERE name = $1
`

//SweepBlobsInNextAccount finds the next account where blobs need to be
//garbage-collected, and performs the GC. This entails a marking of all blobs
//that are not mounted in any repo, and a sweeping of all blobs that were
//marked in the previous pass and which are still not mounted anywhere.
//
//This staged mark-and-sweep ensures that we don't remove fresh blobs
//that were just pushed and have not been mounted anywhere.
//
//Blobs are sweeped in each account at most once per hour. If no accounts need
//to be sweeped, sql.ErrNoRows is returned to instruct the caller to slow down.
func (j *Janitor) SweepBlobsInNextAccount() (returnErr error) {
	var account keppel.Account
	defer func() {
		if returnErr == nil {
			sweepBlobsSuccessCounter.Inc()
		} else if returnErr != sql.ErrNoRows {
			sweepBlobsFailedCounter.Inc()
			returnErr = fmt.Errorf("while sweeping blobs in account %q: %s",
				account.Name, returnErr.Error())
		}
	}()

	//find account to sweep
	maxSweepedAt := j.timeNow().Add(-1 * time.Hour)
	err := j.db.SelectOne(&account, blobSweepSearchQuery, maxSweepedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			logg.Debug("no blobs to sweep - slowing down...")
			return sql.ErrNoRows
		}
		return err
	}

	//NOTE: We don't need to pack the following steps in a single transaction, so
	//we won't. The mark and unmark are obviously safe since they only update
	//metadata, and the sweep only touches stuff that was marked in the
	//*previous* sweep. The only thing that we need to make sure is that unmark
	//is strictly ordered before sweep.
	_, err = j.db.Exec(blobMarkQuery, account.Name, j.timeNow())
	if err != nil {
		return err
	}
	_, err = j.db.Exec(blobUnmarkQuery, account.Name)
	if err != nil {
		return err
	}

	//select blobs for deletion that were marked in the last run 1 hour ago, but
	//use a slightly later cut-off time to account for the marking taking some
	//time
	maxMarkedAt := j.timeNow().Add(-30 * time.Minute)
	var blobs []keppel.Blob
	_, err = j.db.Select(&blobs, blobSelectMarkedQuery, account.Name, maxMarkedAt)
	if err != nil {
		return err
	}

	//delete each blob from the DB *first*, then clean it up on the storage
	//
	//This order is important: The DELETE statement could fail if some concurrent
	//process created a blob mount in the meantime. If that happens, and we have
	//already deleted the blob in the backing storage, we've caused an
	//inconsistency that we cannot recover from. To avoid that risk, we do it the
	//other way around. In this way, we could have an inconsistency where the
	//blob is deleted from the database, but still present in the backing
	//storage. But this inconsistency is easier to recover from:
	//SweepStorageInNextAccount will take care of it soon enough. Also the user
	//will not notice this inconsistency because the DB is our primary source of
	//truth.
	logg.Info("sweeping %d blobs in account %s", len(blobs), account.Name)
	for _, blob := range blobs {
		_, err := j.db.Delete(&blob) //without transaction: we need this committed right now
		if err != nil {
			return err
		}
		err = j.sd.DeleteBlob(account, blob.StorageID)
		if err != nil {
			return err
		}
	}

	_, err = j.db.Exec(blobSweepDoneQuery, account.Name, j.timeNow())
	return err
}
