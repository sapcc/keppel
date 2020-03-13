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

//NOTE: This skips over repos where some or all manifests have failed validation.
//If a manifest fails validation, we cannot be sure that we're really seeing
//all manifest_blob_refs. This could result in us mistakenly deleting blob
//mounts even though they are referenced by a manifest.
const blobMountSweepSearchQuery = `
	SELECT * FROM repos
		WHERE blob_mounts_sweeped_at IS NULL OR blob_mounts_sweeped_at < $1
		AND id NOT IN (SELECT repo_id FROM manifests WHERE validation_error_message != '')
	-- repos without any sweeps first, then sorted by last sweep
	ORDER BY blob_mounts_sweeped_at IS NULL DESC, blob_mounts_sweeped_at ASC
	-- only one repo at a time
	LIMIT 1
`

const blobMountMarkQuery = `
	UPDATE blob_mounts SET marked_for_deletion_at = $2
	WHERE repo_id = $1 AND marked_for_deletion_at IS NULL AND blob_id NOT IN (
		SELECT blob_id FROM manifest_blob_refs WHERE repo_id = $1
	)
`

const blobMountUnmarkQuery = `
	UPDATE blob_mounts SET marked_for_deletion_at = NULL
	WHERE repo_id = $1 AND blob_id IN (
		SELECT blob_id FROM manifest_blob_refs WHERE repo_id = $1
	)
`

const blobMountSweepMarkedQuery = `
	DELETE FROM blob_mounts WHERE repo_id = $1 AND marked_for_deletion_at < $2
`

const blobMountSweepDoneQuery = `
	UPDATE repos SET blob_mounts_sweeped_at = $2 WHERE id = $1
`

//SweepBlobMountsInNextRepo finds the next repo where blob mounts need to be
//garbage-collected, and performs the GC. This entails a marking of all blob
//mounts that are not used by any manifest, and a sweeping of all blob mounts
//that were marked in the previous pass and which are still not used by any
//manifest.
//
//This staged mark-and-sweep ensures that we don't remove fresh blob mounts
//that were just created, but where the manifest has not yet been pushed.
//
//Blob mounts are sweeped in each repo at most once per hour. If no repos need
//to be sweeped, sql.ErrNoRows is returned to instruct the caller to slow down.
func (j *Janitor) SweepBlobMountsInNextRepo() (returnErr error) {
	var repo keppel.Repository
	defer func() {
		if returnErr == nil {
			sweepBlobMountsSuccessCounter.Inc()
		} else if returnErr != sql.ErrNoRows {
			sweepBlobMountsFailedCounter.Inc()
			returnErr = fmt.Errorf("while sweeping blob mounts in repo %q: %s",
				repo.FullName(), returnErr.Error())
		}
	}()

	//find repo to sweep
	maxSweepedAt := j.timeNow().Add(-1 * time.Hour)
	err := j.db.SelectOne(&repo, blobMountSweepSearchQuery, maxSweepedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			logg.Debug("no blob mounts to sweep - slowing down...")
			return sql.ErrNoRows
		}
		return err
	}

	//NOTE: We don't need to pack the following steps in a single transaction, so
	//we won't. The mark and unmark are obviously safe since they only update
	//metadata, and the sweep only touches stuff that was marked in the
	//*previous* sweep. The only thing that we need to make sure is that unmark
	//is strictly ordered before sweep.
	_, err = j.db.Exec(blobMountMarkQuery, repo.ID, j.timeNow())
	if err != nil {
		return err
	}
	_, err = j.db.Exec(blobMountUnmarkQuery, repo.ID)
	if err != nil {
		return err
	}
	//delete blob-mounts that were marked in the last run 1 hour ago, but use a
	//slightly later cut-off time to account for the marking taking some time
	maxMarkedAt := j.timeNow().Add(-30 * time.Minute)
	result, err := j.db.Exec(blobMountSweepMarkedQuery, repo.ID, maxMarkedAt)
	if err != nil {
		return err
	}
	rowsDeleted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsDeleted > 0 {
		logg.Info("%d blob mounts sweeped in repo %s", rowsDeleted, repo.FullName())
	}

	_, err = j.db.Exec(blobMountSweepDoneQuery, repo.ID, j.timeNow())
	return err
}
