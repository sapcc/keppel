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
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/jobloop"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/keppel"
)

// NOTE: This skips over repos where some or all manifests have failed validation.
// If a manifest fails validation, we cannot be sure that we're really seeing
// all manifest_blob_refs. This could result in us mistakenly deleting blob
// mounts even though they are referenced by a manifest.
var blobMountSweepSearchQuery = sqlext.SimplifyWhitespace(`
	SELECT * FROM repos
		WHERE next_blob_mount_sweep_at IS NULL OR next_blob_mount_sweep_at < $1
		AND id NOT IN (SELECT DISTINCT repo_id FROM manifests WHERE validation_error_message != '')
	-- repos without any sweeps first, then sorted by last sweep
	ORDER BY next_blob_mount_sweep_at IS NULL DESC, next_blob_mount_sweep_at ASC
	-- only one repo at a time
	LIMIT 1
`)

var blobMountMarkQuery = sqlext.SimplifyWhitespace(`
	UPDATE blob_mounts SET can_be_deleted_at = $2
	WHERE repo_id = $1 AND can_be_deleted_at IS NULL AND blob_id NOT IN (
		SELECT DISTINCT blob_id FROM manifest_blob_refs WHERE repo_id = $1
	)
`)

var blobMountUnmarkQuery = sqlext.SimplifyWhitespace(`
	UPDATE blob_mounts SET can_be_deleted_at = NULL
	WHERE repo_id = $1 AND blob_id IN (
		SELECT DISTINCT blob_id FROM manifest_blob_refs WHERE repo_id = $1
	)
`)

var blobMountSweepMarkedQuery = sqlext.SimplifyWhitespace(`
	DELETE FROM blob_mounts WHERE repo_id = $1 AND can_be_deleted_at < $2
`)

var blobMountSweepDoneQuery = sqlext.SimplifyWhitespace(`
	UPDATE repos SET next_blob_mount_sweep_at = $2 WHERE id = $1
`)

// BlobMountSweepJob is a job. Each task finds one repo where blob mounts need to be
// garbage-collected, and performs the GC. This entails a marking of all blob
// mounts that are not used by any manifest, and a sweeping of all blob mounts
// that were marked in the previous pass and which are still not used by any
// manifest.
//
// This staged mark-and-sweep ensures that we don't remove fresh blob mounts
// that were just created, but where the manifest has not yet been pushed.
//
// Blob mounts are sweeped in each repo at most once per hour.
func (j *Janitor) BlobMountSweepJob(registerer prometheus.Registerer) jobloop.Job { //nolint:dupl // false positive
	return (&jobloop.ProducerConsumerJob[keppel.Repository]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "garbage collect blob mounts in repos",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_blob_mount_sweeps",
				Help: "Counter for garbage collections on blob mounts in a repo.",
			},
		},
		DiscoverTask: func(ctx context.Context, _ prometheus.Labels) (repo keppel.Repository, err error) {
			err = j.db.WithContext(ctx).SelectOne(&repo, blobMountSweepSearchQuery, j.timeNow())
			return repo, err
		},
		ProcessTask: j.sweepBlobMountsInRepo,
	}).Setup(registerer)
}

func (j *Janitor) sweepBlobMountsInRepo(ctx context.Context, repo keppel.Repository, _ prometheus.Labels) error {
	//allow next pass in 1 hour to delete the newly marked blob mounts, but use a
	//slighly earlier cut-off time to account for the marking taking some time
	canBeDeletedAt := j.timeNow().Add(30 * time.Minute)

	db := j.db.WithContext(ctx)

	//NOTE: We don't need to pack the following steps in a single transaction, so
	//we won't. The mark and unmark are obviously safe since they only update
	//metadata, and the sweep only touches stuff that was marked in the
	//*previous* sweep. The only thing that we need to make sure is that unmark
	//is strictly ordered before sweep.
	_, err := db.Exec(blobMountMarkQuery, repo.ID, canBeDeletedAt)
	if err != nil {
		return err
	}
	_, err = db.Exec(blobMountUnmarkQuery, repo.ID)
	if err != nil {
		return err
	}
	//delete blob-mounts that were marked in the last run
	result, err := db.Exec(blobMountSweepMarkedQuery, repo.ID, j.timeNow())
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

	_, err = db.Exec(blobMountSweepDoneQuery, repo.ID, j.timeNow().Add(j.addJitter(1*time.Hour)))
	return err
}
