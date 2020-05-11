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

	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/keppel"
)

//query that finds the next manifest to be validated
const outdatedManifestSearchQuery = `
	SELECT * FROM manifests WHERE validated_at < $1
	ORDER BY validated_at ASC -- oldest manifests first
	LIMIT 1                   -- one at a time
`

//ValidateNextManifest validates manifests that have not been validated for more
//than 6 hours. At most one manifest is validated per call. If no manifest
//needs to be validated, sql.ErrNoRows is returned.
func (j *Janitor) ValidateNextManifest() (returnErr error) {
	defer func() {
		if returnErr == nil {
			validateManifestSuccessCounter.Inc()
		} else if returnErr != sql.ErrNoRows {
			validateManifestFailedCounter.Inc()
			returnErr = fmt.Errorf("while validating a manifest: %s", returnErr.Error())
		}
	}()

	//find manifest
	var manifest keppel.Manifest
	maxValidatedAt := j.timeNow().Add(-6 * time.Hour)
	err := j.db.SelectOne(&manifest, outdatedManifestSearchQuery, maxValidatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			logg.Debug("no manifests to validate - slowing down...")
			return sql.ErrNoRows
		}
		return err
	}

	//find corresponding account and repo
	var repo keppel.Repository
	err = j.db.SelectOne(&repo, `SELECT * FROM repos WHERE id = $1`, manifest.RepositoryID)
	if err != nil {
		return fmt.Errorf("cannot find repo %d for manifest %s: %s", manifest.RepositoryID, manifest.Digest, err.Error())
	}
	account, err := keppel.FindAccount(j.db, repo.AccountName)
	if err != nil {
		return fmt.Errorf("cannot find account for manifest %s/%s: %s", repo.FullName(), manifest.Digest, err.Error())
	}

	//perform validation
	err = j.processor().ValidateExistingManifest(*account, repo, &manifest, j.timeNow())
	if err == nil {
		//update `validated_at` and reset error message
		_, err := j.db.Exec(`
			UPDATE manifests SET validated_at = $1, validation_error_message = ''
			 WHERE repo_id = $2 AND digest = $3`,
			j.timeNow(), repo.ID, manifest.Digest,
		)
		if err != nil {
			return err
		}
	} else {
		//attempt to log the error message, and also update the `validated_at`
		//timestamp to ensure that the ValidateNextManifest() loop does not get
		//stuck on this one
		_, updateErr := j.db.Exec(`
			UPDATE manifests SET validated_at = $1, validation_error_message = $2
			 WHERE repo_id = $3 AND digest = $4`,
			j.timeNow(), err.Error(), repo.ID, manifest.Digest,
		)
		if updateErr != nil {
			err = fmt.Errorf("%s (additional error encountered while recording validation error: %s)", err.Error(), updateErr.Error())
		}
		return err
	}

	return nil
}

const syncManifestRepoSelectQuery = `
	SELECT r.* FROM repos r
		JOIN accounts a ON r.account_name = a.name
		WHERE (r.next_manifest_sync_at IS NULL OR r.next_manifest_sync_at < $1)
		-- only consider repos in replica accounts
		AND a.upstream_peer_hostname != ''
	-- repos without any syncs first, then sorted by last sync
	ORDER BY r.next_manifest_sync_at IS NULL DESC, r.next_manifest_sync_at ASC
	-- only one repo at a time
	LIMIT 1
`

const syncManifestEnumerateRefsQuery = `
	SELECT parent_digest, child_digest FROM manifest_manifest_refs WHERE repo_id = $1
`

const syncManifestDoneQuery = `
	UPDATE repos SET next_manifest_sync_at = $2 WHERE id = $1
`

//SyncManifestsInNextRepo finds the next repository in a replica account where
//manifests have not been synced for more than an hour, and syncs its manifests.
//Syncing involves checking with the primary account which manifests have been
//deleted there, and replicating the deletions on our side.
//
//If no repo needs syncing, sql.ErrNoRows is returned.
func (j *Janitor) SyncManifestsInNextRepo() (returnErr error) {
	defer func() {
		if returnErr == nil {
			syncManifestsSuccessCounter.Inc()
		} else if returnErr != sql.ErrNoRows {
			syncManifestsFailedCounter.Inc()
			returnErr = fmt.Errorf("while syncing manifests in a replica repo: %s", returnErr.Error())
		}
	}()

	//find repository to sync
	var repo keppel.Repository
	err := j.db.SelectOne(&repo, syncManifestRepoSelectQuery, j.timeNow())
	if err != nil {
		if err == sql.ErrNoRows {
			logg.Debug("no accounts to sync manifests in - slowing down...")
			return sql.ErrNoRows
		}
		return err
	}

	//find corresponding account
	account, err := keppel.FindAccount(j.db, repo.AccountName)
	if err != nil {
		return fmt.Errorf("cannot find account for repo %s: %s", repo.FullName(), err.Error())
	}

	//do not perform manifest sync while account is in maintenance (maintenance mode blocks all kinds of replication)
	if !account.InMaintenance {
		err = j.performManifestSync(*account, repo)
		if err != nil {
			return err
		}
	}

	_, err = j.db.Exec(syncManifestDoneQuery, repo.ID, j.timeNow().Add(1*time.Hour))
	return err
}

func (j *Janitor) performManifestSync(account keppel.Account, repo keppel.Repository) error {
	//enumerate manifests in this repo
	var manifests []keppel.Manifest
	_, err := j.db.Select(&manifests, `SELECT * FROM manifests WHERE repo_id = $1`, repo.ID)
	if err != nil {
		return fmt.Errorf("cannot list manifests in repo %s: %s", repo.FullName(), err.Error())
	}

	//check which manifests need to be deleted
	shallDeleteManifest := make(map[string]bool)
	p := j.processor()
	for _, manifest := range manifests {
		ref := keppel.ManifestReference{Digest: digest.Digest(manifest.Digest)}
		exists, err := p.CheckManifestOnPrimary(account, repo, ref)
		if err != nil {
			return fmt.Errorf("cannot check existence of manifest %s/%s on primary account: %s", repo.FullName(), manifest.Digest, err.Error())
		}
		if !exists {
			shallDeleteManifest[manifest.Digest] = true
		}
	}

	//enumerate manifest-manifest refs in this repo
	parentDigestsOf := make(map[string][]string)
	err = keppel.ForeachRow(j.db, syncManifestEnumerateRefsQuery, []interface{}{repo.ID}, func(rows *sql.Rows) error {
		var (
			parentDigest string
			childDigest  string
		)
		err = rows.Scan(&parentDigest, &childDigest)
		if err != nil {
			return err
		}
		parentDigestsOf[childDigest] = append(parentDigestsOf[childDigest], parentDigest)
		return nil
	})
	if err != nil {
		return fmt.Errorf("cannot enumerate manifest-manifest refs in repo %s: %s", repo.FullName(), err.Error())
	}

	//delete manifests in correct order (if there is a parent-child relationship,
	//we always need to delete the parent manifest first, otherwise the database
	//will complain because of its consistency checks)
	if len(shallDeleteManifest) > 0 {
		logg.Info("deleting %d manifests in repo %s that were deleted on corresponding primary account", len(shallDeleteManifest), repo.FullName())
	}
	manifestWasDeleted := make(map[string]bool)
	for len(shallDeleteManifest) > 0 {
		deletedSomething := false
	MANIFEST:
		for digest := range shallDeleteManifest {
			for _, parentDigest := range parentDigestsOf[digest] {
				if !manifestWasDeleted[parentDigest] {
					//cannot delete this manifest yet because it's still being referenced - retry in next iteration
					continue MANIFEST
				}
			}

			//no manifests left that reference this one - we can delete it
			//
			//The ordering is important: The DELETE statement could fail if some concurrent
			//process created a manifest reference in the meantime. If that happens,
			//and we have already deleted the manifest in the backing storage, we've
			//caused an inconsistency that we cannot recover from. To avoid that
			//risk, we do it the other way around. In this way, we could have an
			//inconsistency where the manifest is deleted from the database, but still
			//present in the backing storage. But this inconsistency is easier to
			//recover from: SweepStorageInNextAccount will take care of it soon
			//enough. Also the user will not notice this inconsistency because the DB
			//is our primary source of truth.
			_, err := j.db.Delete(&keppel.Manifest{RepositoryID: repo.ID, Digest: digest}) //without transaction: we need this committed right now

			if err != nil {
				return fmt.Errorf("cannot remove deleted manifest %s in repo %s from DB: %s", digest, repo.FullName(), err.Error())
			}
			err = j.sd.DeleteManifest(account, repo.Name, digest)
			if err != nil {
				return fmt.Errorf("cannot remove deleted manifest %s in repo %s from storage: %s", digest, repo.FullName(), err.Error())
			}

			//remove deletion from work queue (so that we can eventually exit from the outermost loop)
			delete(shallDeleteManifest, digest)

			//track deletion (so that we can eventually start deleting manifests referenced by this one)
			manifestWasDeleted[digest] = true

			//track that we're making progress
			deletedSomething = true
		}

		//we should be deleting something in each iteration, otherwise we will get stuck in an infinite loop
		if !deletedSomething {
			undeletedDigests := make([]string, 0, len(shallDeleteManifest))
			for digest := range shallDeleteManifest {
				undeletedDigests = append(undeletedDigests, digest)
			}
			return fmt.Errorf("cannot remove deleted manifests %v in repo %s because they are still being referenced by other manifests (this smells like an inconsistency on the primary account)",
				undeletedDigests, repo.FullName())
		}
	}

	return nil
}
