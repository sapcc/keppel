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

var blobSweepSearchQuery = sqlext.SimplifyWhitespace(`
	SELECT * FROM accounts
		WHERE next_blob_sweep_at IS NULL OR next_blob_sweep_at < $1
	-- accounts without any sweeps first, then sorted by last sweep
	ORDER BY next_blob_sweep_at IS NULL DESC, next_blob_sweep_at ASC
	-- only one account at a time
	LIMIT 1
`)

var blobMarkQuery = sqlext.SimplifyWhitespace(`
	UPDATE blobs SET can_be_deleted_at = $2
	WHERE account_name = $1 AND can_be_deleted_at IS NULL AND id NOT IN (
		SELECT m.blob_id FROM blob_mounts m JOIN repos r ON m.repo_id = r.id
		WHERE r.account_name = $1
	)
`)

var blobUnmarkQuery = sqlext.SimplifyWhitespace(`
	UPDATE blobs SET can_be_deleted_at = NULL
	WHERE account_name = $1 AND id IN (
		SELECT m.blob_id FROM blob_mounts m JOIN repos r ON m.repo_id = r.id
		WHERE r.account_name = $1
	)
`)

var blobSelectMarkedQuery = sqlext.SimplifyWhitespace(`
	SELECT * FROM blobs WHERE account_name = $1 AND can_be_deleted_at < $2
`)

var blobSweepDoneQuery = sqlext.SimplifyWhitespace(`
	UPDATE accounts SET next_blob_sweep_at = $2 WHERE name = $1
`)

// SweepBlobsInNextAccount finds the next account where blobs need to be
// garbage-collected, and performs the GC. This entails a marking of all blobs
// that are not mounted in any repo, and a sweeping of all blobs that were
// marked in the previous pass and which are still not mounted anywhere.
//
// This staged mark-and-sweep ensures that we don't remove fresh blobs
// that were just pushed and have not been mounted anywhere.
//
// Blobs are sweeped in each account at most once per hour. If no accounts need
// to be sweeped, sql.ErrNoRows is returned to instruct the caller to slow down.
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
	err := j.db.SelectOne(&account, blobSweepSearchQuery, j.timeNow())
	if err != nil {
		if err == sql.ErrNoRows {
			logg.Debug("no blobs to sweep - slowing down...")
			return sql.ErrNoRows
		}
		return err
	}

	//allow next pass in 1 hour to delete the newly marked blob mounts, but use a
	//slighly earlier cut-off time to account for the marking taking some time
	canBeDeletedAt := j.timeNow().Add(30 * time.Minute)

	//NOTE: We don't need to pack the following steps in a single transaction, so
	//we won't. The mark and unmark are obviously safe since they only update
	//metadata, and the sweep only touches stuff that was marked in the
	//*previous* sweep. The only thing that we need to make sure is that unmark
	//is strictly ordered before sweep.
	_, err = j.db.Exec(blobMarkQuery, account.Name, canBeDeletedAt)
	if err != nil {
		return err
	}
	_, err = j.db.Exec(blobUnmarkQuery, account.Name)
	if err != nil {
		return err
	}

	//select blobs for deletion that were marked in the last run
	var blobs []keppel.Blob
	_, err = j.db.Select(&blobs, blobSelectMarkedQuery, account.Name, j.timeNow())
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
	if len(blobs) > 0 {
		logg.Info("sweeping %d blobs in account %s", len(blobs), account.Name)
	}
	for _, blob := range blobs {
		//without transaction: we need this committed right now
		_, err := j.db.Delete(&blob) //nolint:gosec // Delete is not holding onto the pointer after it returns
		if err != nil {
			return err
		}
		if blob.StorageID != "" { //ignore unbacked blobs that were never replicated
			err = j.sd.DeleteBlob(account, blob.StorageID)
			if err != nil {
				return err
			}
		}
	}

	_, err = j.db.Exec(blobSweepDoneQuery, account.Name, j.timeNow().Add(j.addJitter(1*time.Hour)))
	return err
}

var validateBlobSearchQuery = sqlext.SimplifyWhitespace(`
	SELECT * FROM blobs
		WHERE storage_id != '' AND (validated_at < $1 OR (validated_at < $2 AND validation_error_message != ''))
	ORDER BY validation_error_message != '' DESC, validated_at ASC
		-- oldest blobs first, but always prefer to recheck a failed validation
	LIMIT 1
		-- one at a time
`)

// ValidateNextBlob validates the next blob that has not been validated for more
// than 7 days. If no manifest needs to be validated, sql.ErrNoRows is returned.
func (j *Janitor) ValidateNextBlob() (returnErr error) {
	defer func() {
		if returnErr == nil {
			validateBlobSuccessCounter.Inc()
		} else if returnErr != sql.ErrNoRows {
			validateBlobFailedCounter.Inc()
			returnErr = fmt.Errorf("while validating a blob: %s", returnErr.Error())
		}
	}()

	//find blob: validate once every 7 days, but recheck after 10 minutes if
	//validation failed
	var blob keppel.Blob
	maxSuccessfulValidatedAt := j.timeNow().Add(-7 * 24 * time.Hour)
	maxFailedValidatedAt := j.timeNow().Add(-10 * time.Minute)
	err := j.db.SelectOne(&blob, validateBlobSearchQuery, maxSuccessfulValidatedAt, maxFailedValidatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			logg.Debug("no blobs to validate - slowing down...")
			return sql.ErrNoRows
		}
		return err
	}

	//find corresponding account
	account, err := keppel.FindAccount(j.db, blob.AccountName)
	if err != nil {
		return fmt.Errorf("cannot find account for manifest %s/%s: %s", blob.AccountName, blob.Digest, err.Error())
	}

	//perform validation
	err = j.processor().ValidateExistingBlob(*account, blob)
	if err == nil {
		//update `validated_at` and reset error message
		_, err := j.db.Exec(`
			UPDATE blobs SET validated_at = $1, validation_error_message = ''
			 WHERE account_name = $2 AND digest = $3`,
			j.timeNow(), account.Name, blob.Digest,
		)
		if err != nil {
			return err
		}
	} else {
		//attempt to log the error message, and also update the `validated_at`
		//timestamp to ensure that the ValidateNextBlob() loop does not get stuck
		//on this one
		_, updateErr := j.db.Exec(`
			UPDATE blobs SET validated_at = $1, validation_error_message = $2
			 WHERE account_name = $3 AND digest = $4`,
			j.timeNow(), err.Error(), account.Name, blob.Digest,
		)
		if updateErr != nil {
			err = fmt.Errorf("%s (additional error encountered while recording validation error: %s)", err.Error(), updateErr.Error())
		}
		return err
	}

	return nil
}
