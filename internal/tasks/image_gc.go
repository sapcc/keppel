// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	. "github.com/majewsky/gg/option"
	"github.com/opencontainers/go-digest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/errext"
	"github.com/sapcc/go-bits/jobloop"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/processor"
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

// ManifestGarbageCollectionJob is a job. Each task finds the a where GC has
// not been performed for more than an hour, and performs GC based on the GC
// policies configured on the repo's account.
func (j *Janitor) ManifestGarbageCollectionJob(registerer prometheus.Registerer) jobloop.Job { //nolint: dupl // interface implementation of different things
	return (&jobloop.ProducerConsumerJob[models.Repository]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "manifest garbage collection",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_image_garbage_collections",
				Help: "Counter for image garbage collection runs in repos.",
			},
		},
		DiscoverTask: func(_ context.Context, _ prometheus.Labels) (repo models.Repository, err error) {
			err = j.db.SelectOne(&repo, imageGCRepoSelectQuery, j.timeNow())
			return repo, err
		},
		ProcessTask: j.garbageCollectManifestsInRepo,
	}).Setup(registerer)
}

func (j *Janitor) garbageCollectManifestsInRepo(ctx context.Context, repo models.Repository, labels prometheus.Labels) (returnErr error) {
	// load GC policies for this repository
	account, err := keppel.FindAccount(j.db, repo.AccountName)
	if err != nil {
		return fmt.Errorf("cannot find account for repo %s: %w", repo.FullName(), err)
	}
	gcPolicies, err := keppel.ParseGCPolicies(*account)
	if err != nil {
		return fmt.Errorf("cannot load GC policies for account %s: %w", account.Name, err)
	}
	var gcPoliciesForRepo []keppel.GCPolicy
	for idx, gcPolicy := range gcPolicies {
		err := gcPolicy.Validate()
		if err != nil {
			return fmt.Errorf("GC policy #%d for account %s is invalid: %w", idx+1, account.Name, err)
		}
		if gcPolicy.MatchesRepository(repo.Name) {
			gcPoliciesForRepo = append(gcPoliciesForRepo, gcPolicy)
		}
	}

	// execute GC policies
	if len(gcPoliciesForRepo) > 0 {
		tagPolicies, err := keppel.ParseTagPolicies(account.TagPoliciesJSON)
		if err != nil {
			return err
		}
		err = j.executeGCPolicies(ctx, account.Reduced(), repo, gcPoliciesForRepo, tagPolicies)
		if err != nil {
			return err
		}
	} else {
		// if there are no policies to apply, we can skip a whole bunch of work, but
		// we still need to update the GCStatusJSON field on the repo's manifests to
		// make sure those statuses don't refer to deleted GC policies
		_, err = j.db.Exec(imageGCResetStatusQuery, repo.ID)
		if err != nil {
			return err
		}
	}

	_, err = j.db.Exec(imageGCRepoDoneQuery, repo.ID, j.timeNow().Add(j.addJitter(1*time.Hour)))
	return err
}

type manifestData struct {
	Manifest      models.Manifest
	TagNames      []string
	ParentDigests []string
	GCStatus      keppel.GCStatus
	IsDeleted     bool
}

func (j *Janitor) executeGCPolicies(ctx context.Context, account models.ReducedAccount, repo models.Repository, gcPolicies []keppel.GCPolicy, tagPolicies []keppel.TagPolicy) error {
	// load manifests in repo
	var dbManifests []models.Manifest
	_, err := j.db.Select(&dbManifests, `SELECT * FROM manifests WHERE repo_id = $1`, repo.ID)
	if err != nil {
		return err
	}

	// setup a bit of structure to track state in during the policy evaluation
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

	// load tags (for matching policies on match_tag, except_tag and only_untagged)
	query := `SELECT digest, name FROM tags WHERE repo_id = $1`
	err = sqlext.ForeachRow(j.db, query, []any{repo.ID}, func(rows *sql.Rows) error {
		var (
			digest  digest.Digest
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

	// check manifest-manifest relations to fill GCStatus.ProtectedByManifest
	query = `SELECT parent_digest, child_digest FROM manifest_manifest_refs WHERE repo_id = $1`
	err = sqlext.ForeachRow(j.db, query, []any{repo.ID}, func(rows *sql.Rows) error {
		var (
			parentDigest string
			childDigest  digest.Digest
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
			sort.Strings(m.ParentDigests) // for deterministic test behavior
			m.GCStatus.ProtectedByParentManifest = m.ParentDigests[0]
		}
	}

	// check if the subject target digest manifest exists
outer:
	for _, manifest := range manifests {
		if manifest.Manifest.SubjectDigest == "" {
			continue
		}

		for _, m := range manifests {
			if m.Manifest.Digest == manifest.Manifest.SubjectDigest {
				manifest.GCStatus.ProtectedBySubjectManifest = manifest.Manifest.SubjectDigest.String()
				continue outer
			}
		}
	}

	// evaluate policies in order
	proc := j.processor()
	for _, gcPolicy := range gcPolicies {
		err := j.evaluatePolicy(ctx, proc, manifests, account, repo, gcPolicy, tagPolicies)
		if err != nil {
			return err
		}
	}

	return j.persistGCStatus(manifests, repo.ID)
}

func (j *Janitor) evaluatePolicy(ctx context.Context, proc *processor.Processor, manifests []*manifestData, account models.ReducedAccount, repo models.Repository, gcPolicy keppel.GCPolicy, tagPolicies []keppel.TagPolicy) error {
	// for some time constraint matches, we need to know which manifests are
	// still alive
	var aliveManifests []models.Manifest
	for _, m := range manifests {
		if !m.IsDeleted {
			aliveManifests = append(aliveManifests, m.Manifest)
		}
	}

	// evaluate policy for each manifest
	for _, m := range manifests {
		// skip those manifests that are already deleted, and those which are
		// protected by an earlier policy or one of the baseline checks above
		if m.IsDeleted || m.GCStatus.IsProtected() {
			continue
		}

		// track matching "delete" policies in GCStatus to allow users insight
		// into how policies match
		if gcPolicy.Action == "delete" {
			m.GCStatus.RelevantGCPolicies = append(m.GCStatus.RelevantGCPolicies, gcPolicy)
		}

		// evaluate constraints
		if !gcPolicy.MatchesTags(m.TagNames) {
			continue
		}
		if !gcPolicy.MatchesTimeConstraint(m.Manifest, aliveManifests, j.timeNow()) {
			continue
		}

		// execute policy action
		switch gcPolicy.Action {
		case "protect":
			m.GCStatus.ProtectedByGCPolicy = Some(gcPolicy)
		case "delete":
			err := proc.DeleteManifest(ctx, account, repo, m.Manifest.Digest, tagPolicies, keppel.AuditContext{
				UserIdentity: janitorUserIdentity{
					TaskName: "policy-driven-gc",
					GCPolicy: Some(gcPolicy),
				},
				Request: janitorDummyRequest,
			})
			if tagPolicyError, ok := errext.As[processor.DeleteManifestBlockedByTagPolicyError](err); ok {
				m.GCStatus.ProtectedByTagPolicy = Some(tagPolicyError.Policy)
				continue
			}
			if err != nil {
				return err
			}
			m.IsDeleted = true
			policyJSON, _ := json.Marshal(gcPolicy)
			logg.Info("GC on repo %s: deleted manifest %s because of policy %s", repo.FullName(), m.Manifest.Digest, string(policyJSON))
		default:
			// defense in depth: we already did p.Validate() earlier
			return fmt.Errorf("unexpected GC policy action: %q (why was this not caught by Validate!?)", gcPolicy.Action)
		}
	}

	return nil
}

func (j *Janitor) persistGCStatus(manifests []*manifestData, repoID int64) error {
	// finalize and persist GCStatus for all affected manifests
	query := `UPDATE manifests SET gc_status_json = $1 WHERE repo_id = $2 AND digest = $3`
	err := sqlext.WithPreparedStatement(j.db, query, func(stmt *sql.Stmt) error {
		for _, m := range manifests {
			if m.IsDeleted {
				continue
			}
			// to simplify UI, show only EITHER protection status OR relevant deleting
			// policies, not both
			if m.GCStatus.IsProtected() {
				m.GCStatus.RelevantGCPolicies = nil
			}
			gcStatusJSON, err := json.Marshal(m.GCStatus)
			if err != nil {
				return err
			}
			_, err = stmt.Exec(string(gcStatusJSON), repoID, m.Manifest.Digest)
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
