/******************************************************************************
*
*  Copyright 2021 SAP SE
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

var imageGCRepoSelectQuery = keppel.SimplifyWhitespaceInSQL(`
	SELECT * FROM repos
		WHERE (next_gc_at IS NULL OR next_gc_at < $1)
	-- repos without any syncs first, then sorted by last sync
	ORDER BY next_gc_at IS NULL DESC, next_gc_at ASC
	-- only one repo at a time
	LIMIT 1
`)

var imageGCRepoDoneQuery = keppel.SimplifyWhitespaceInSQL(`
	UPDATE repos SET next_gc_at = $2 WHERE id = $1
`)

//GarbageCollectManifestsInNextRepo finds the next repository where GC has not been performed for more than an hour, and
func (j *Janitor) GarbageCollectManifestsInNextRepo() (returnErr error) {
	var repo keppel.Repository

	defer func() {
		if returnErr == nil {
			imageGCSuccessCounter.Inc()
		} else if returnErr != sql.ErrNoRows {
			imageGCFailedCounter.Inc()
			repoFullName := repo.FullName()
			if repoFullName == "" {
				repoFullName = "unknown"
			}
			returnErr = fmt.Errorf("while GCing manifests in the repo %s: %w", repoFullName, returnErr)
		}
	}()

	//find repository to sync
	err := j.db.SelectOne(&repo, imageGCRepoSelectQuery, j.timeNow())
	if err != nil {
		if err == sql.ErrNoRows {
			logg.Debug("no accounts to sync manifests in - slowing down...")
			return sql.ErrNoRows
		}
		return err
	}

	//load GC policies for this account
	account, err := keppel.FindAccount(j.db, repo.AccountName)
	if err != nil {
		return fmt.Errorf("cannot find account for repo %s: %w", repo.FullName(), err)
	}
	policies, err := account.ParseGCPolicies()
	if err != nil {
		return fmt.Errorf("cannot load GC policies for account %s: %w", account.Name, err)
	}
	for idx, policy := range policies {
		err := policy.Validate()
		if err != nil {
			return fmt.Errorf("GC policy #%d for account %s is invalid: %w", idx+1, account.Name, err)
		}
	}

	//find matching GC policies
	doDeleteUntagged := false
	for _, policy := range policies {
		if !policy.MatchesRepository(repo.Name) {
			continue
		}
		if policy.OnlyUntagged && policy.Action == "delete" {
			doDeleteUntagged = true
		}
		//TODO evaluate all other policies
	}

	//execute selected GC passes (we do this outside the above for loop to
	//tightly control the order of passes)
	if doDeleteUntagged {
		err := j.deleteUntaggedImages(*account, repo)
		if err != nil {
			return fmt.Errorf("while deleting untagged images: %w", err)
		}
	}

	_, err = j.db.Exec(imageGCRepoDoneQuery, repo.ID, j.timeNow().Add(1*time.Hour))
	return err
}

var untaggedImagesSelectQuery = keppel.SimplifyWhitespaceInSQL(`
	SELECT * FROM manifests
		WHERE repo_id = $1
		-- only consider untagged images
		AND digest NOT IN (SELECT digest FROM tags WHERE repo_id = $1)
		-- never cleanup images that are part of another image
		AND digest NOT IN (SELECT child_digest FROM manifest_manifest_refs WHERE repo_id = $1)
		-- do not consider freshly pushed images (the client may still be working on pushing the tag)
		AND pushed_at < $2
`)

func (j *Janitor) deleteUntaggedImages(account keppel.Account, repo keppel.Repository) error {
	var manifests []keppel.Manifest
	_, err := j.db.Select(&manifests, untaggedImagesSelectQuery, repo.ID, j.timeNow().Add(-5*time.Minute))
	if err != nil {
		return err
	}

	proc := j.processor()
	for _, manifest := range manifests {
		err := proc.DeleteManifest(account, repo, manifest.Digest, keppel.AuditContext{
			Authorization: keppel.JanitorAuthorization{TaskName: "gc-untagged-images"},
			Request:       janitorDummyRequest,
		})
		if err != nil {
			return err
		}
	}

	return nil
}
