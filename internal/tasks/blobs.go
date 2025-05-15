// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/jobloop"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
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

// BlobSweepJob is a job. Each task finds one account where blobs need to be
// garbage-collected, and performs the GC. This entails a marking of all blobs
// that are not mounted in any repo, and a sweeping of all blobs that were
// marked in the previous pass and which are still not mounted anywhere.
//
// This staged mark-and-sweep ensures that we don't remove fresh blobs
// that were just pushed and have not been mounted anywhere.
//
// Blobs are sweeped in each account at most once per hour.
func (j *Janitor) BlobSweepJob(registerer prometheus.Registerer) jobloop.Job { //nolint:dupl // false positive
	return (&jobloop.ProducerConsumerJob[models.Account]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "sweep blobs",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_blob_sweeps",
				Help: "Counter for garbage collections on blobs in an account.",
			},
		},
		DiscoverTask: func(_ context.Context, _ prometheus.Labels) (account models.Account, err error) {
			err = j.db.SelectOne(&account, blobSweepSearchQuery, j.timeNow())
			return account, err
		},
		ProcessTask: j.sweepBlobsInRepo,
	}).Setup(registerer)
}

func (j *Janitor) sweepBlobsInRepo(ctx context.Context, account models.Account, _ prometheus.Labels) error {
	// allow next pass in 1 hour to delete the newly marked blob mounts, but use a
	// slightly earlier cut-off time to account for the marking taking some time
	canBeDeletedAt := j.timeNow().Add(30 * time.Minute)

	//NOTE: We don't need to pack the following steps in a single transaction, so
	// we won't. The mark and unmark are obviously safe since they only update
	// metadata, and the sweep only touches stuff that was marked in the
	// *previous* sweep. The only thing that we need to make sure is that unmark
	// is strictly ordered before sweep.
	_, err := j.db.Exec(blobMarkQuery, account.Name, canBeDeletedAt)
	if err != nil {
		return err
	}
	_, err = j.db.Exec(blobUnmarkQuery, account.Name)
	if err != nil {
		return err
	}

	// select blobs for deletion that were marked in the last run
	var blobs []models.Blob
	_, err = j.db.Select(&blobs, blobSelectMarkedQuery, account.Name, j.timeNow())
	if err != nil {
		return err
	}

	// delete each blob from the DB *first*, then clean it up on the storage
	//
	// This order is important: The DELETE statement could fail if some concurrent
	// process created a blob mount in the meantime. If that happens, and we have
	// already deleted the blob in the backing storage, we've caused an
	// inconsistency that we cannot recover from. To avoid that risk, we do it the
	// other way around. In this way, we could have an inconsistency where the
	// blob is deleted from the database, but still present in the backing
	// storage. But this inconsistency is easier to recover from:
	// StorageSweepJob will take care of it soon enough. Also the user
	// will not notice this inconsistency because the DB is our primary source of
	// truth.
	if len(blobs) > 0 {
		logg.Info("sweeping %d blobs in account %s", len(blobs), account.Name)
	}
	for _, blob := range blobs {
		// without transaction: we need this committed right now
		_, err := j.db.Delete(&blob)
		if err != nil {
			return err
		}
		if blob.StorageID != "" { // ignore unbacked blobs that were never replicated
			err = j.sd.DeleteBlob(ctx, account.Reduced(), blob.StorageID)
			if err != nil {
				return err
			}
		}
	}

	_, err = j.db.Exec(blobSweepDoneQuery, account.Name, j.timeNow().Add(j.addJitter(1*time.Hour)))
	return err
}

var validateBlobSearchQuery = sqlext.SimplifyWhitespace(`
	SELECT * FROM blobs WHERE storage_id != '' AND next_validation_at < $1
	ORDER BY next_validation_at ASC
	LIMIT 1 -- one at a time
`)

var validateBlobFinishQuery = sqlext.SimplifyWhitespace(`
	UPDATE blobs SET next_validation_at = $1, validation_error_message = $2
	WHERE account_name = $3 AND digest = $4
`)

// BlobValidationJob is a job. Each task validates a blob that has not been validated for more
// than 7 days.
//
//nolint:dupl
func (j *Janitor) BlobValidationJob(registerer prometheus.Registerer) jobloop.Job {
	return (&jobloop.ProducerConsumerJob[models.Blob]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "validation of blob contents",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_blob_validations",
				Help: "Counter for blob validations.",
			},
		},
		DiscoverTask: func(_ context.Context, _ prometheus.Labels) (blob models.Blob, err error) {
			err = j.db.SelectOne(&blob, validateBlobSearchQuery, j.timeNow())
			return blob, err
		},
		ProcessTask: j.validateBlob,
	}).Setup(registerer)
}

func (j *Janitor) validateBlob(ctx context.Context, blob models.Blob, _ prometheus.Labels) error {
	// find corresponding account
	account, err := keppel.FindAccount(j.db, blob.AccountName)
	if err != nil {
		return fmt.Errorf("cannot find account for manifest %s/%s: %s", blob.AccountName, blob.Digest, err.Error())
	}

	// perform validation
	err = j.processor().ValidateExistingBlob(ctx, account.Reduced(), blob)
	if err == nil {
		// on success, reset error message and schedule next validation
		_, err := j.db.Exec(validateBlobFinishQuery,
			j.timeNow().Add(j.addJitter(models.BlobValidationInterval)),
			"", account.Name, blob.Digest,
		)
		if err != nil {
			return err
		}
	} else {
		// on failure, log error message and schedule next validation sooner than usual
		_, updateErr := j.db.Exec(validateBlobFinishQuery,
			j.timeNow().Add(j.addJitter(models.BlobValidationAfterErrorInterval)),
			err.Error(), account.Name, blob.Digest,
		)
		if updateErr != nil {
			err = fmt.Errorf("%w (additional error encountered while recording validation error: %s)", err, updateErr.Error())
		}
		return err
	}

	return nil
}
