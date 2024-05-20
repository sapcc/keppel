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
	"database/sql"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/jobloop"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

var storageSweepSearchQuery = sqlext.SimplifyWhitespace(`
	SELECT * FROM accounts
		WHERE next_storage_sweep_at IS NULL OR next_storage_sweep_at < $1
	-- accounts without any sweeps first, then sorted by last sweep
	ORDER BY next_storage_sweep_at IS NULL DESC, next_storage_sweep_at ASC
	-- only one account at a time
	LIMIT 1
`)

var storageSweepDoneQuery = sqlext.SimplifyWhitespace(`
	UPDATE accounts SET next_storage_sweep_at = $2 WHERE name = $1
`)

// SweepStorageJob is a job. Each task finds an account where the backing storage
// needs to be garbage-collected, and performs the GC. This entails a marking of
// all blobs and manifests that exist in the backing storage, but not in the
// database; and a sweeping of all items that were marked in the previous pass
// and which are still not entered in the database.
//
// This staged mark-and-sweep ensures that we don't remove fresh blobs and
// manifests that were just pushed, but where the entry in the database is still
// being created.
//
// The storage of each account is sweeped at most once every 6 hours.
func (j *Janitor) StorageSweepJob(registerer prometheus.Registerer) jobloop.Job { //nolint:dupl // false positive
	return (&jobloop.ProducerConsumerJob[models.Account]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "storage sweep",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_storage_sweeps",
				Help: "Counter for garbage collections of an account's backing storage.",
			},
		},
		DiscoverTask: func(_ context.Context, _ prometheus.Labels) (account models.Account, err error) {
			err = j.db.SelectOne(&account, storageSweepSearchQuery, j.timeNow())
			return account, err
		},
		ProcessTask: j.sweepStorage,
	}).Setup(registerer)
}

func (j *Janitor) sweepStorage(_ context.Context, account models.Account, _ prometheus.Labels) error {
	// enumerate blobs and manifests in the backing storage
	actualBlobs, actualManifests, err := j.sd.ListStorageContents(account)
	if err != nil {
		return err
	}

	// when creating new entries in `unknown_blobs` and `unknown_manifests`, set
	// the `can_be_deleted_at` timestamp such that the next pass 6 hours from now
	// will sweep them (we don't use .Add(6 * time.Hour) to account for the
	// marking taking some time)
	canBeDeletedAt := j.timeNow().Add(4 * time.Hour)

	// handle blobs and manifests separately
	err = j.sweepBlobStorage(account, actualBlobs, canBeDeletedAt)
	if err != nil {
		return err
	}
	err = j.sweepManifestStorage(account, actualManifests, canBeDeletedAt)
	if err != nil {
		return err
	}

	_, err = j.db.Exec(storageSweepDoneQuery, account.Name, j.timeNow().Add(j.addJitter(6*time.Hour)))
	return err
}

func (j *Janitor) sweepBlobStorage(account models.Account, actualBlobs []keppel.StoredBlobInfo, canBeDeletedAt time.Time) error {
	actualBlobsByStorageID := make(map[string]keppel.StoredBlobInfo, len(actualBlobs))
	for _, blobInfo := range actualBlobs {
		actualBlobsByStorageID[blobInfo.StorageID] = blobInfo
	}

	// enumerate blobs known to the DB
	isKnownStorageID := make(map[string]bool)
	query := `SELECT storage_id FROM blobs WHERE account_name = $1`
	err := sqlext.ForeachRow(j.db, query, []any{account.Name}, func(rows *sql.Rows) error {
		var storageID string
		err := rows.Scan(&storageID)
		isKnownStorageID[storageID] = true
		return err
	})
	if err != nil {
		return err
	}

	// blobs in the backing storage may also correspond to uploads in progress
	query = `SELECT storage_id FROM uploads WHERE repo_id IN (SELECT id FROM repos WHERE account_name = $1)`
	err = sqlext.ForeachRow(j.db, query, []any{account.Name}, func(rows *sql.Rows) error {
		var storageID string
		err := rows.Scan(&storageID)
		isKnownStorageID[storageID] = true
		return err
	})
	if err != nil {
		return err
	}

	// unmark/sweep phase: enumerate all unknown blobs
	var unknownBlobs []models.UnknownBlob
	_, err = j.db.Select(&unknownBlobs, `SELECT * FROM unknown_blobs WHERE account_name = $1`, account.Name)
	if err != nil {
		return err
	}
	isMarkedStorageID := make(map[string]bool)
	for _, unknownBlob := range unknownBlobs {
		// unmark blobs that have been recorded in the database in the meantime
		if isKnownStorageID[unknownBlob.StorageID] {
			_, err = j.db.Delete(&unknownBlob)
			if err != nil {
				return err
			}
			continue
		}

		// sweep blobs that have been marked long enough
		isMarkedStorageID[unknownBlob.StorageID] = true
		if unknownBlob.CanBeDeletedAt.Before(j.timeNow()) {
			// only call DeleteBlob if we can still see the blob in the backing
			// storage (this protects against unexpected errors e.g. because an
			// operator deleted the blob between the mark and sweep phases, or if we
			// deleted the blob from the backing storage in a previous sweep, but
			// could not remove the unknown_blobs entry from the DB)
			if blobInfo, exists := actualBlobsByStorageID[unknownBlob.StorageID]; exists {
				// need to use different cleanup strategies depending on whether the
				// blob upload was finalized or not
				if blobInfo.ChunkCount > 0 {
					logg.Info("storage sweep in account %s: removing unfinalized blob stored at %s with %d chunks",
						account.Name, unknownBlob.StorageID, blobInfo.ChunkCount)
					err = j.sd.AbortBlobUpload(account, unknownBlob.StorageID, blobInfo.ChunkCount)
				} else {
					logg.Info("storage sweep in account %s: removing finalized blob stored at %s",
						account.Name, unknownBlob.StorageID)
					err = j.sd.DeleteBlob(account, unknownBlob.StorageID)
				}
				if err != nil {
					return err
				}
			}
			_, err = j.db.Delete(&unknownBlob)
			if err != nil {
				return err
			}
		}
	}

	// mark phase: record newly discovered unknown blobs in the DB
	for storageID := range actualBlobsByStorageID {
		if isKnownStorageID[storageID] || isMarkedStorageID[storageID] {
			continue
		}
		err := j.db.Insert(&models.UnknownBlob{
			AccountName:    account.Name,
			StorageID:      storageID,
			CanBeDeletedAt: canBeDeletedAt,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (j *Janitor) sweepManifestStorage(account models.Account, actualManifests []keppel.StoredManifestInfo, canBeDeletedAt time.Time) error {
	isActualManifest := make(map[keppel.StoredManifestInfo]bool, len(actualManifests))
	for _, m := range actualManifests {
		isActualManifest[m] = true
	}

	// enumerate manifests known to the DB
	isKnownManifest := make(map[keppel.StoredManifestInfo]bool)
	query := `SELECT r.name, m.digest FROM repos r JOIN manifests m ON m.repo_id = r.id WHERE r.account_name = $1`
	err := sqlext.ForeachRow(j.db, query, []any{account.Name}, func(rows *sql.Rows) error {
		var m keppel.StoredManifestInfo
		err := rows.Scan(&m.RepoName, &m.Digest)
		isKnownManifest[m] = true
		return err
	})
	if err != nil {
		return err
	}

	// unmark/sweep phase: enumerate all unknown manifests
	var unknownManifests []models.UnknownManifest
	_, err = j.db.Select(&unknownManifests, `SELECT * FROM unknown_manifests WHERE account_name = $1`, account.Name)
	if err != nil {
		return err
	}
	isMarkedManifest := make(map[keppel.StoredManifestInfo]bool)
	for _, unknownManifest := range unknownManifests {
		unknownManifestInfo := keppel.StoredManifestInfo{
			RepoName: unknownManifest.RepositoryName,
			Digest:   unknownManifest.Digest,
		}

		// unmark manifests that have been recorded in the database in the meantime
		if isKnownManifest[unknownManifestInfo] {
			_, err = j.db.Delete(&unknownManifest)
			if err != nil {
				return err
			}
			continue
		}

		// sweep manifests that have been marked long enough
		isMarkedManifest[unknownManifestInfo] = true
		if unknownManifest.CanBeDeletedAt.Before(j.timeNow()) {
			// only call DeleteManifest if we can still see the manifest in the
			// backing storage (this protects against unexpected errors e.g. because
			// an operator deleted the manifest between the mark and sweep phases, or
			// if we deleted the manifest from the backing storage in a previous
			// sweep, but could not remove the unknown_manifests entry from the DB)
			if isActualManifest[unknownManifestInfo] {
				logg.Info("storage sweep in account %s: removing manifest %s/%s",
					account.Name, unknownManifest.RepositoryName, unknownManifest.Digest)
				err := j.sd.DeleteManifest(account, unknownManifest.RepositoryName, unknownManifest.Digest)
				if err != nil {
					return err
				}
			}
			_, err = j.db.Delete(&unknownManifest)
			if err != nil {
				return err
			}
		}
	}

	// mark phase: record newly discovered unknown manifests in the DB
	for manifest := range isActualManifest {
		if isKnownManifest[manifest] || isMarkedManifest[manifest] {
			continue
		}
		err := j.db.Insert(&models.UnknownManifest{
			AccountName:    account.Name,
			RepositoryName: manifest.RepoName,
			Digest:         manifest.Digest,
			CanBeDeletedAt: canBeDeletedAt,
		})
		if err != nil {
			return err
		}
	}

	return nil
}
