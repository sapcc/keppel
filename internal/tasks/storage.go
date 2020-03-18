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

const storageSweepSearchQuery = `
	SELECT * FROM accounts
		WHERE storage_sweeped_at IS NULL OR storage_sweeped_at < $1
	-- accounts without any sweeps first, then sorted by last sweep
	ORDER BY storage_sweeped_at IS NULL DESC, storage_sweeped_at ASC
	-- only one account at a time
	LIMIT 1
`

const storageSweepDoneQuery = `
	UPDATE accounts SET storage_sweeped_at = $2 WHERE name = $1
`

//SweepStorageInNextAccount finds the next account where the backing storage
//needs to be garbage-collected, and performs the GC. This entails a marking of
//all blobs and manifests that exist in the backing storage, but not in the
//database; and a sweeping of all items that were marked in the previous pass
//and which are still not entered in the database.
//
//This staged mark-and-sweep ensures that we don't remove fresh blobs and
//manifests that were just pushed, but where the entry in the database is still
//being created.
//
//The storage of each account is sweeped at most once every 6 hours. If no
//accounts need to be sweeped, sql.ErrNoRows is returned to instruct the caller
//to slow down.
func (j *Janitor) SweepStorageInNextAccount() (returnErr error) {
	var account keppel.Account
	defer func() {
		if returnErr == nil {
			sweepStorageSuccessCounter.Inc()
		} else if returnErr != sql.ErrNoRows {
			sweepStorageFailedCounter.Inc()
			returnErr = fmt.Errorf("while sweeping storage in account %q: %s",
				account.Name, returnErr.Error())
		}
	}()

	//find account to sweep
	maxSweepedAt := j.timeNow().Add(-6 * time.Hour)
	err := j.db.SelectOne(&account, storageSweepSearchQuery, maxSweepedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			logg.Debug("no storages to sweep - slowing down...")
			return sql.ErrNoRows
		}
		return err
	}

	//enumerate blobs and manifests in the backing storage
	actualStorageIDs, actualManifests, err := j.sd.ListStorageContents(account)
	if err != nil {
		return err
	}

	//we want to delete everything that was marked in the last run 6 hours ago,
	//but use a slightly later cut-off time to account for the marking taking
	//some time
	maxMarkedAt := j.timeNow().Add(-4 * time.Hour)

	//handle blobs and manifests separately
	err = j.sweepBlobStorage(account, actualStorageIDs, maxMarkedAt)
	if err != nil {
		return err
	}
	err = j.sweepManifestStorage(account, actualManifests, maxMarkedAt)
	if err != nil {
		return err
	}

	_, err = j.db.Exec(storageSweepDoneQuery, account.Name, j.timeNow())
	return err
}

func (j *Janitor) sweepBlobStorage(account keppel.Account, actualStorageIDs []string, maxMarkedAt time.Time) error {
	isActualStorageID := make(map[string]bool, len(actualStorageIDs))
	for _, id := range actualStorageIDs {
		isActualStorageID[id] = true
	}

	//enumerate blobs known to the DB
	isKnownStorageID := make(map[string]bool)
	query := `SELECT storage_id FROM blobs WHERE account_name = $1`
	err := keppel.ForeachRow(j.db, query, []interface{}{account.Name}, func(rows *sql.Rows) error {
		var storageID string
		err := rows.Scan(&storageID)
		isKnownStorageID[storageID] = true
		return err
	})
	if err != nil {
		return err
	}

	//blobs in the backing storage may also correspond to uploads in progress
	query = `SELECT storage_id FROM uploads WHERE repo_id IN (SELECT id FROM repos WHERE account_name = $1)`
	err = keppel.ForeachRow(j.db, query, []interface{}{account.Name}, func(rows *sql.Rows) error {
		var storageID string
		err := rows.Scan(&storageID)
		isKnownStorageID[storageID] = true
		return err
	})
	if err != nil {
		return err
	}

	//unmark/sweep phase: enumerate all unknown blobs
	var unknownBlobs []keppel.UnknownBlob
	_, err = j.db.Select(&unknownBlobs, `SELECT * FROM unknown_blobs WHERE account_name = $1`, account.Name)
	if err != nil {
		return err
	}
	isMarkedStorageID := make(map[string]bool)
	for _, unknownBlob := range unknownBlobs {
		//unmark blobs that have been recorded in the database in the meantime
		if isKnownStorageID[unknownBlob.StorageID] {
			_, err = j.db.Delete(&unknownBlob)
			if err != nil {
				return err
			}
			continue
		}

		//sweep blobs that have been marked long enough
		isMarkedStorageID[unknownBlob.StorageID] = true
		if unknownBlob.MarkedForDeletionAt.Before(maxMarkedAt) {
			//only call DeleteBlob if we can still see the blob in the backing
			//storage (this protects against unexpected errors e.g. because an
			//operator deleted the blob between the mark and sweep phases, or if we
			//deleted the blob from the backing storage in a previous sweep, but
			//could not remove the unknown_blobs entry from the DB)
			if isActualStorageID[unknownBlob.StorageID] {
				//TODO We might need to call AbortBlobUpload() instead.
				err := j.sd.DeleteBlob(account, unknownBlob.StorageID)
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

	//mark phase: record newly discovered unknown blobs in the DB
	for storageID := range isActualStorageID {
		if isKnownStorageID[storageID] || isMarkedStorageID[storageID] {
			continue
		}
		err := j.db.Insert(&keppel.UnknownBlob{
			AccountName:         account.Name,
			StorageID:           storageID,
			MarkedForDeletionAt: j.timeNow(),
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (j *Janitor) sweepManifestStorage(account keppel.Account, actualManifests []keppel.StoredManifestInfo, maxMarkedAt time.Time) error {
	isActualManifest := make(map[keppel.StoredManifestInfo]bool, len(actualManifests))
	for _, m := range actualManifests {
		isActualManifest[m] = true
	}

	//enumerate manifests known to the DB
	isKnownManifest := make(map[keppel.StoredManifestInfo]bool)
	query := `SELECT r.name, m.digest FROM repos r JOIN manifests m ON m.repo_id = r.id WHERE r.account_name = $1`
	err := keppel.ForeachRow(j.db, query, []interface{}{account.Name}, func(rows *sql.Rows) error {
		var m keppel.StoredManifestInfo
		err := rows.Scan(&m.RepoName, &m.Digest)
		isKnownManifest[m] = true
		return err
	})
	if err != nil {
		return err
	}

	//unmark/sweep phase: enumerate all unknown manifests
	var unknownManifests []keppel.UnknownManifest
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

		//unmark manifests that have been recorded in the database in the meantime
		if isKnownManifest[unknownManifestInfo] {
			_, err = j.db.Delete(&unknownManifest)
			if err != nil {
				return err
			}
			continue
		}

		//sweep manifests that have been marked long enough
		isMarkedManifest[unknownManifestInfo] = true
		if unknownManifest.MarkedForDeletionAt.Before(maxMarkedAt) {
			//only call DeleteManifest if we can still see the manifest in the
			//backing storage (this protects against unexpected errors e.g. because
			//an operator deleted the manifest between the mark and sweep phases, or
			//if we deleted the manifest from the backing storage in a previous
			//sweep, but could not remove the unknown_manifests entry from the DB)
			if isActualManifest[unknownManifestInfo] {
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

	//mark phase: record newly discovered unknown manifests in the DB
	for manifest := range isActualManifest {
		if isKnownManifest[manifest] || isMarkedManifest[manifest] {
			continue
		}
		err := j.db.Insert(&keppel.UnknownManifest{
			AccountName:         account.Name,
			RepositoryName:      manifest.RepoName,
			Digest:              manifest.Digest,
			MarkedForDeletionAt: j.timeNow(),
		})
		if err != nil {
			return err
		}
	}

	return nil
}
