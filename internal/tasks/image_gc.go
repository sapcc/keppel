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
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/keppel"
)

var imageGCRepoSelectQuery = sqlext.SimplifyWhitespace(`
	SELECT * FROM repos
		WHERE (next_gc_at IS NULL OR next_gc_at < $1)
	-- repos without any syncs first, then sorted by last sync
	ORDER BY next_gc_at IS NULL DESC, next_gc_at ASC
	-- only one repo at a time
	LIMIT 1
`)

var imageGCResetStatusQuery = sqlext.SimplifyWhitespace(`
	UPDATE manifests SET gc_status_json = '{"relevant_policies":[]}' WHERE repo_id = $1
`)

var imageGCRepoDoneQuery = sqlext.SimplifyWhitespace(`
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

	//load GC policies for this repository
	account, err := keppel.FindAccount(j.db, repo.AccountName)
	if err != nil {
		return fmt.Errorf("cannot find account for repo %s: %w", repo.FullName(), err)
	}
	policies, err := account.ParseGCPolicies()
	if err != nil {
		return fmt.Errorf("cannot load GC policies for account %s: %w", account.Name, err)
	}
	var policiesForRepo []keppel.GCPolicy
	for idx, policy := range policies {
		err := policy.Validate()
		if err != nil {
			return fmt.Errorf("GC policy #%d for account %s is invalid: %w", idx+1, account.Name, err)
		}
		if policy.MatchesRepository(repo.Name) {
			policiesForRepo = append(policiesForRepo, policy)
		}
	}

	//execute GC policies
	if len(policiesForRepo) > 0 {
		err = j.executeGCPolicies(*account, repo, policiesForRepo)
		if err != nil {
			return err
		}
	} else {
		//if there are no policies to apply, we can skip a whole bunch of work, but
		//we still need to update the GCStatusJSON field on the repo's manifests to
		//make sure those statuses don't refer to deleted GC policies
		_, err = j.db.Exec(imageGCResetStatusQuery, repo.ID)
		if err != nil {
			return err
		}
	}

	_, err = j.db.Exec(imageGCRepoDoneQuery, repo.ID, j.timeNow().Add(1*time.Hour))
	return err
}

func (j *Janitor) executeGCPolicies(account keppel.Account, repo keppel.Repository, policies []keppel.GCPolicy) error {
	//load manifests in repo
	var dbManifests []keppel.Manifest
	_, err := j.db.Select(&dbManifests, `SELECT * FROM manifests WHERE repo_id = $1`, repo.ID)
	if err != nil {
		return err
	}

	//setup a bit of structure to track state in during the policy evaluation
	type manifestData struct {
		Manifest      keppel.Manifest
		TagNames      []string
		ParentDigests []string
		GCStatus      keppel.GCStatus
		IsDeleted     bool
	}
	var manifests []*manifestData
	for _, m := range dbManifests {
		manifests = append(manifests, &manifestData{
			Manifest: m,
			GCStatus: keppel.GCStatus{
				ProtectedByRecentUpload: m.PushedAt.After(j.timeNow().Add(-10 * time.Minute)),
			},
			IsDeleted: false,
		})
	}

	//load tags (for matching policies on match_tag, except_tag and only_untagged)
	query := `SELECT digest, name FROM tags WHERE repo_id = $1`
	err = sqlext.ForeachRow(j.db, query, []interface{}{repo.ID}, func(rows *sql.Rows) error {
		var (
			digest  string
			tagName string
		)
		err := rows.Scan(&digest, &tagName)
		if err != nil {
			return err
		}
		for _, m := range manifests {
			if m.Manifest.Digest == digest {
				m.TagNames = append(m.TagNames, tagName)
				break
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	//check manifest-manifest relations to fill GCStatus.ProtectedByManifest
	query = `SELECT parent_digest, child_digest FROM manifest_manifest_refs WHERE repo_id = $1`
	err = sqlext.ForeachRow(j.db, query, []interface{}{repo.ID}, func(rows *sql.Rows) error {
		var (
			parentDigest string
			childDigest  string
		)
		err := rows.Scan(&parentDigest, &childDigest)
		if err != nil {
			return err
		}
		for _, m := range manifests {
			if m.Manifest.Digest == childDigest {
				m.ParentDigests = append(m.ParentDigests, parentDigest)
				break
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, m := range manifests {
		if len(m.ParentDigests) > 0 {
			sort.Strings(m.ParentDigests) //for deterministic test behavior
			m.GCStatus.ProtectedByParentManifest = m.ParentDigests[0]
		}
	}

	//evaulate policies in order
	proc := j.processor()
	for _, p := range policies {
		//for some time constraint matches, we need to know which manifests are
		//still alive
		var aliveManifests []keppel.Manifest
		for _, m := range manifests {
			if !m.IsDeleted {
				aliveManifests = append(aliveManifests, m.Manifest)
			}
		}

		//evaluate policy for each manifest
		for _, m := range manifests {
			//skip those manifests that are already deleted, and those which are
			//protected by an earlier policy or one of the baseline checks above
			if m.IsDeleted || m.GCStatus.IsProtected() {
				continue
			}

			//track matching "delete" policies in GCStatus to allow users insight
			//into how policies match
			if p.Action == "delete" {
				m.GCStatus.RelevantPolicies = append(m.GCStatus.RelevantPolicies, p)
			}

			//evaluate constraints
			if !p.MatchesTags(m.TagNames) {
				continue
			}
			if !p.MatchesTimeConstraint(m.Manifest, aliveManifests, j.timeNow()) {
				continue
			}

			//execute policy action
			switch p.Action {
			case "protect":
				pCopied := p
				m.GCStatus.ProtectedByPolicy = &pCopied
			case "delete":
				err := proc.DeleteManifest(account, repo, m.Manifest.Digest, keppel.AuditContext{
					UserIdentity: janitorUserIdentity{
						TaskName: "policy-driven-gc",
						GCPolicy: &p,
					},
					Request: janitorDummyRequest,
				})
				if err != nil {
					return err
				}
				m.IsDeleted = true
				policyJSON, _ := json.Marshal(p)
				logg.Info("GC on repo %s: deleted manifest %s because of policy %s", repo.FullName(), m.Manifest.Digest, string(policyJSON))
			default:
				//defense in depth: we already did p.Validate() earlier
				return fmt.Errorf("unexpected GC policy action: %q (why was this not caught by Validate!?)", p.Action)
			}
		}
	}

	//finalize and persist GCStatus for all affected manifests
	query = `UPDATE manifests SET gc_status_json = $1 WHERE repo_id = $2 AND digest = $3`
	err = sqlext.WithPreparedStatement(j.db, query, func(stmt *sql.Stmt) error {
		for _, m := range manifests {
			if m.IsDeleted {
				continue
			}
			//to simplify UI, show only EITHER protection status OR relevant deleting
			//policies, not both
			if m.GCStatus.IsProtected() {
				m.GCStatus.RelevantPolicies = nil
			}
			gcStatusJSON, err := json.Marshal(m.GCStatus)
			if err != nil {
				return err
			}
			_, err = stmt.Exec(string(gcStatusJSON), repo.ID, m.Manifest.Digest)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("while persisting GCStatus: %w", err)
	}
	return nil
}
