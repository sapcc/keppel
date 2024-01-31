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
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/docker/distribution/manifest/schema2"
	"github.com/go-gorp/gorp/v3"
	"github.com/opencontainers/go-digest"
	imageSpecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/errext"
	"github.com/sapcc/go-bits/jobloop"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sqlext"
	"golang.org/x/exp/maps"

	"github.com/sapcc/keppel/internal/auth"
	peerclient "github.com/sapcc/keppel/internal/client/peer"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/processor"
	"github.com/sapcc/keppel/internal/trivy"
)

// query that finds the next manifest to be validated
var outdatedManifestSearchQuery = sqlext.SimplifyWhitespace(`
	SELECT * FROM manifests
		WHERE validated_at < $1 OR (validated_at < $2 AND validation_error_message != '')
	ORDER BY validation_error_message != '' DESC, validated_at ASC, media_type DESC
		-- oldest blobs first, but always prefer to recheck a failed validation (see below for why we sort by media_type)
	LIMIT 1
		-- one at a time
`)

//^ NOTE: The sorting by media_type is completely useless in real-world
//situations since real-life manifests will always have validated_at timestamps
//that differ at least by some nanoseconds. But in tests, this sorting ensures
//that single-arch images get revalidated before multi-arch images, which is
//important because multi-arch images take into account the size_bytes
//attribute of their constituent images.

// ManifestValidationJob is a job. Each task validates a manifest that has not been validated for more
// than 24 hours.
func (j *Janitor) ManifestValidationJob(registerer prometheus.Registerer) jobloop.Job {
	return (&jobloop.ProducerConsumerJob[keppel.Manifest]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "manifest validation",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_manifest_validations",
				Help: "Counter for manifest validations.",
			},
		},
		DiscoverTask: func(ctx context.Context, _ prometheus.Labels) (manifest keppel.Manifest, err error) {
			//find manifest: validate once every 24 hours, but recheck after 10 minutes if validation failed
			maxSuccessfulValidatedAt := j.timeNow().Add(-24 * time.Hour)
			maxFailedValidatedAt := j.timeNow().Add(-10 * time.Minute)
			err = j.db.WithContext(ctx).SelectOne(&manifest, outdatedManifestSearchQuery, maxSuccessfulValidatedAt, maxFailedValidatedAt)
			return manifest, err
		},
		ProcessTask: j.validateManifest,
	}).Setup(registerer)
}

func (j *Janitor) validateManifest(ctx context.Context, manifest keppel.Manifest, _ prometheus.Labels) error {
	db := j.db.WithContext(ctx)
	//find corresponding account and repo
	var repo keppel.Repository
	err := db.SelectOne(&repo, `SELECT * FROM repos WHERE id = $1`, manifest.RepositoryID)
	if err != nil {
		return fmt.Errorf("cannot find repo %d for manifest %s: %w", manifest.RepositoryID, manifest.Digest, err)
	}
	account, err := keppel.FindAccount(j.db, repo.AccountName)
	if err != nil {
		return fmt.Errorf("cannot find account for manifest %s/%s: %w", repo.FullName(), manifest.Digest, err)
	}

	//perform validation
	err = j.processor().ValidateExistingManifest(*account, repo, &manifest, j.timeNow())
	if err != nil {
		//attempt to log the error message, and also update the `validated_at`
		//timestamp to ensure that the ManifestValidationJob does not get
		//stuck on this one
		_, updateErr := db.Exec(`
			UPDATE manifests SET validated_at = $1, validation_error_message = $2
				WHERE repo_id = $3 AND digest = $4`,
			j.timeNow(), err.Error(), repo.ID, manifest.Digest,
		)
		if updateErr != nil {
			err = fmt.Errorf("%w (additional error encountered while recording validation error: %w)", err, updateErr)
		}
		return fmt.Errorf("while validating manifest %s in repo %d: %w", manifest.Digest, manifest.RepositoryID, err)
	}

	//update `validated_at` and reset error message
	_, err = db.Exec(`
			UPDATE manifests SET validated_at = $1, validation_error_message = ''
				WHERE repo_id = $2 AND digest = $3`,
		j.timeNow(), repo.ID, manifest.Digest,
	)
	return err
}

var syncManifestRepoSelectQuery = sqlext.SimplifyWhitespace(`
	SELECT r.* FROM repos r
		JOIN accounts a ON r.account_name = a.name
		WHERE (r.next_manifest_sync_at IS NULL OR r.next_manifest_sync_at < $1)
		-- only consider repos in replica accounts
		AND (a.upstream_peer_hostname != '' OR a.external_peer_url != '')
	-- repos without any syncs first, then sorted by last sync
	ORDER BY r.next_manifest_sync_at IS NULL DESC, r.next_manifest_sync_at ASC
	-- only one repo at a time
	LIMIT 1
`)

var syncManifestEnumerateRefsQuery = sqlext.SimplifyWhitespace(`
	SELECT parent_digest, child_digest FROM manifest_manifest_refs WHERE repo_id = $1
`)

var syncManifestDoneQuery = sqlext.SimplifyWhitespace(`
	UPDATE repos SET next_manifest_sync_at = $2 WHERE id = $1
`)

var syncManifestCleanupEmptyQuery = sqlext.SimplifyWhitespace(`
	DELETE FROM repos r WHERE id = $1 AND (SELECT COUNT(*) FROM manifests WHERE repo_id = r.id) = 0
`)

// ManifestSyncJob is a job. Each task finds a repository in a replica account where
// manifests have not been synced for more than an hour, and syncs its manifests.
// Syncing involves checking with the primary account which manifests have been
// deleted there, and replicating the deletions on our side.
func (j *Janitor) ManifestSyncJob(registerer prometheus.Registerer) jobloop.Job { //nolint:dupl // false positive
	return (&jobloop.ProducerConsumerJob[keppel.Repository]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "manifest sync in replica repos",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_manifest_syncs",
				Help: "Counter for manifest syncs in replica repos.",
			},
		},
		DiscoverTask: func(ctx context.Context, _ prometheus.Labels) (repo keppel.Repository, err error) {
			err = j.db.WithContext(ctx).SelectOne(&repo, syncManifestRepoSelectQuery, j.timeNow())
			return repo, err
		},
		ProcessTask: j.syncManifestsInReplicaRepo,
	}).Setup(registerer)
}

func (j *Janitor) syncManifestsInReplicaRepo(ctx context.Context, repo keppel.Repository, _ prometheus.Labels) error {
	//find corresponding account
	account, err := keppel.FindAccount(j.db, repo.AccountName)
	if err != nil {
		return fmt.Errorf("cannot find account for repo %s: %w", repo.FullName(), err)
	}

	//do not perform manifest sync while account is in maintenance (maintenance mode blocks all kinds of replication)
	if !account.InMaintenance {
		syncPayload, err := j.getReplicaSyncPayload(ctx, *account, repo)
		if err != nil {
			return err
		}
		err = j.performTagSync(ctx, *account, repo, syncPayload)
		if err != nil {
			return fmt.Errorf("while syncing tags in repo %s: %w", repo.FullName(), err)
		}
		err = j.performManifestSync(ctx, *account, repo, syncPayload)
		if err != nil {
			return fmt.Errorf("while syncing manifests in repo %s: %w", repo.FullName(), err)
		}
	}

	db := j.db.WithContext(ctx)
	_, err = db.Exec(syncManifestDoneQuery, repo.ID, j.timeNow().Add(j.addJitter(1*time.Hour)))
	if err != nil {
		return err
	}
	_, err = db.Exec(syncManifestCleanupEmptyQuery, repo.ID)
	return err
}

// When performing a manifest/tag sync, and the upstream is one of our peers,
// we can use the replica-sync API instead of polling each manifest and tag
// individually. This also synchronizes our own last_pulled_at timestamps into
// the primary account. The primary therefore gains a complete picture of pull
// activity, which is required for some GC policies to work correctly.
func (j *Janitor) getReplicaSyncPayload(ctx context.Context, account keppel.Account, repo keppel.Repository) (*keppel.ReplicaSyncPayload, error) {
	//the replica-sync API is only available when upstream is a peer
	if account.UpstreamPeerHostName == "" {
		return nil, nil
	}

	//get peer
	var peer keppel.Peer
	err := j.db.WithContext(ctx).SelectOne(&peer, `SELECT * FROM peers WHERE hostname = $1`, account.UpstreamPeerHostName)
	if err != nil {
		return nil, err
	}

	//get token for peer
	client, err := peerclient.New(ctx, j.cfg, peer, auth.PeerAPIScope)
	if err != nil {
		return nil, err
	}

	//assemble request body
	tagsByDigest := make(map[digest.Digest][]keppel.TagForSync)
	query := `SELECT name, digest, last_pulled_at FROM tags WHERE repo_id = $1`
	err = sqlext.ForeachRow(j.db, query, []interface{}{repo.ID}, func(rows *sql.Rows) error {
		var (
			name         string
			digest       digest.Digest
			lastPulledAt *time.Time
		)
		err = rows.Scan(&name, &digest, &lastPulledAt)
		if err != nil {
			return err
		}
		tagsByDigest[digest] = append(tagsByDigest[digest], keppel.TagForSync{
			Name:         name,
			LastPulledAt: keppel.MaybeTimeToUnix(lastPulledAt),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	var manifests []keppel.ManifestForSync
	query = `SELECT digest, last_pulled_at FROM manifests WHERE repo_id = $1`
	err = sqlext.ForeachRow(j.db, query, []interface{}{repo.ID}, func(rows *sql.Rows) error {
		var (
			digest       digest.Digest
			lastPulledAt *time.Time
		)
		err = rows.Scan(&digest, &lastPulledAt)
		if err != nil {
			return err
		}
		manifests = append(manifests, keppel.ManifestForSync{
			Digest:       digest,
			LastPulledAt: keppel.MaybeTimeToUnix(lastPulledAt),
			Tags:         tagsByDigest[digest],
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	return client.PerformReplicaSync(ctx, repo.FullName(), keppel.ReplicaSyncPayload{Manifests: manifests})
}

func (j *Janitor) performTagSync(ctx context.Context, account keppel.Account, repo keppel.Repository, syncPayload *keppel.ReplicaSyncPayload) error {
	db := j.db.WithContext(ctx)

	var tags []keppel.Tag
	_, err := db.Select(&tags, `SELECT * FROM tags WHERE repo_id = $1`, repo.ID)
	if err != nil {
		return fmt.Errorf("cannot list tags: %w", err)
	}

	p := j.processor()
TAG:
	for _, tag := range tags {
		//if we have a ReplicaSyncPayload available, use it
		if syncPayload != nil {
			switch syncPayload.DigestForTag(tag.Name) {
			case tag.Digest:
				//the tag still points to the same digest - nothing to do
				continue TAG
			case "":
				//the tag was deleted - replicate the tag deletion into our replica
				_, err := db.Delete(&tag) //nolint:gosec // Delete is not holding onto the pointer after it returns
				if err != nil {
					return err
				}
				continue TAG
			default:
				//the tag was updated to point to a different manifest - replicate it
				//using the generic codepath below
				break
			}
		}

		//we want to check if upstream still has the tag, and if it has moved to a
		//different manifest, replicate that manifest; all of that boils down to
		//just a ReplicateManifest() call
		ref := models.ManifestReference{Tag: tag.Name}
		_, _, err := p.ReplicateManifest(ctx, account, repo, ref, keppel.AuditContext{
			UserIdentity: janitorUserIdentity{TaskName: "tag-sync"},
			Request:      janitorDummyRequest,
		})
		if err != nil {
			//if the tag itself (and only the tag itself!) 404s, we can replicate the tag deletion into our replica
			err404, ok := errext.As[processor.UpstreamManifestMissingError](err)
			if ok && err404.Ref == ref {
				_, err := db.Delete(&tag) //nolint:gosec // Delete is not holding onto the pointer after it returns
				if err != nil {
					return err
				}
			} else {
				//all other errors fail the sync
				return fmt.Errorf("while syncing tag %s: %w", tag.Name, err)
			}
		}
	}

	return nil
}

var repoUntaggedManifestsSelectQuery = sqlext.SimplifyWhitespace(`
	SELECT m.* FROM manifests m
		WHERE repo_id = $1
		AND digest NOT IN (SELECT DISTINCT digest FROM tags WHERE repo_id = $1)
`)

func (j *Janitor) performManifestSync(ctx context.Context, account keppel.Account, repo keppel.Repository, syncPayload *keppel.ReplicaSyncPayload) error {
	//enumerate manifests in this repo (this only needs to consider untagged
	//manifests: we run right after performTagSync, therefore all images that are
	//tagged right now were already confirmed to still be good)
	var manifests []keppel.Manifest
	_, err := j.db.WithContext(ctx).Select(&manifests, repoUntaggedManifestsSelectQuery, repo.ID)
	if err != nil {
		return fmt.Errorf("cannot list manifests: %w", err)
	}

	//check which manifests need to be deleted
	shallDeleteManifest := make(map[digest.Digest]bool)
	p := j.processor()
	for _, manifest := range manifests {
		//if we have a ReplicaSyncPayload available, use it to check manifest existence
		if syncPayload != nil {
			if !syncPayload.HasManifest(manifest.Digest) {
				shallDeleteManifest[manifest.Digest] = true
			}
			continue
		}

		//when querying an external registry, we have to check each manifest one-by-one
		ref := models.ManifestReference{Digest: manifest.Digest}
		exists, err := p.CheckManifestOnPrimary(ctx, account, repo, ref)
		if err != nil {
			return fmt.Errorf("cannot check existence of manifest %s on primary account: %w", manifest.Digest, err)
		}
		if !exists {
			shallDeleteManifest[manifest.Digest] = true
		}
	}

	//if nothing needs to be deleted, we're done here
	if len(shallDeleteManifest) == 0 {
		return nil
	}

	//enumerate manifest-manifest refs in this repo
	parentDigestsOf := make(map[digest.Digest][]digest.Digest)
	err = sqlext.ForeachRow(j.db, syncManifestEnumerateRefsQuery, []interface{}{repo.ID}, func(rows *sql.Rows) error {
		var (
			parentDigest digest.Digest
			childDigest  digest.Digest
		)
		err = rows.Scan(&parentDigest, &childDigest)
		if err != nil {
			return err
		}
		parentDigestsOf[childDigest] = append(parentDigestsOf[childDigest], parentDigest)
		return nil
	})
	if err != nil {
		return fmt.Errorf("cannot enumerate manifest-manifest refs: %w", err)
	}

	//delete manifests in correct order (if there is a parent-child relationship,
	//we always need to delete the parent manifest first, otherwise the database
	//will complain because of its consistency checks)
	if len(shallDeleteManifest) > 0 {
		logg.Info("deleting %d manifests in repo %s that were deleted on corresponding primary account", len(shallDeleteManifest), repo.FullName())
	}
	manifestWasDeleted := make(map[digest.Digest]bool)
	for len(shallDeleteManifest) > 0 {
		deletedSomething := false
	MANIFEST:
		for digestToBeDeleted := range shallDeleteManifest {
			if slices.ContainsFunc(parentDigestsOf[digestToBeDeleted], func(parentDigest digest.Digest) bool { return !manifestWasDeleted[parentDigest] }) {
				//cannot delete this manifest yet because it's still being referenced - retry in next iteration
				continue MANIFEST
			}

			//no manifests left that reference this one - we can delete it
			err := j.processor().DeleteManifest(ctx, account, repo, digestToBeDeleted, keppel.AuditContext{
				UserIdentity: janitorUserIdentity{TaskName: "manifest-sync"},
				Request:      janitorDummyRequest,
			})
			if err != nil {
				return fmt.Errorf("cannot remove deleted manifest %s: %w", digestToBeDeleted, err)
			}

			//remove deletion from work queue (so that we can eventually exit from the outermost loop)
			delete(shallDeleteManifest, digestToBeDeleted)

			//track deletion (so that we can eventually start deleting manifests referenced by this one)
			manifestWasDeleted[digestToBeDeleted] = true

			//track that we're making progress
			deletedSomething = true
		}

		//we should be deleting something in each iteration, otherwise we will get stuck in an infinite loop
		if !deletedSomething {
			return fmt.Errorf("cannot remove deleted manifests %v because they are still being referenced by other manifests (this smells like an inconsistency on the primary account)",
				maps.Keys(shallDeleteManifest))
		}
	}

	return nil
}

var vulnCheckBlobSelectQuery = sqlext.SimplifyWhitespace(`
	SELECT b.* FROM blobs b
	JOIN manifest_blob_refs r ON b.id = r.blob_id
		WHERE r.repo_id = $1 AND r.digest = $2
`)

func (j *Janitor) collectManifestLayerBlobs(ctx context.Context, account keppel.Account, repo keppel.Repository, manifest keppel.Manifest) (layerBlobs []keppel.Blob, err error) {
	var blobs []keppel.Blob
	_, err = j.db.WithContext(ctx).Select(&blobs, vulnCheckBlobSelectQuery, manifest.RepositoryID, manifest.Digest)
	if err != nil {
		return nil, err
	}

	// we only care about blobs that are image layers; the manifest tells us which blobs are layers
	manifestBytes, err := j.sd.ReadManifest(account, repo.Name, manifest.Digest)
	if err != nil {
		return nil, err
	}
	manifestParsed, manifestDesc, err := keppel.ParseManifest(manifest.MediaType, manifestBytes)
	if err != nil {
		return nil, keppel.ErrManifestInvalid.With(err.Error())
	}
	if manifest.Digest != "" && manifestDesc.Digest != manifest.Digest {
		return nil, keppel.ErrDigestInvalid.With("actual manifest digest is %s", manifestDesc.Digest)
	}
	isLayer := make(map[digest.Digest]bool)
	for _, desc := range manifestParsed.FindImageLayerBlobs() {
		isLayer[desc.Digest] = true
	}

	for _, blob := range blobs {
		if isLayer[blob.Digest] {
			layerBlobs = append(layerBlobs, blob)
		}
	}

	return
}

const (
	trivySecurityInfoBatchSize = 50
	trivySecurityInfoThreads   = 10
)

var securityCheckSelectQuery = sqlext.SimplifyWhitespace(fmt.Sprintf(`
	SELECT * FROM trivy_security_info
	WHERE next_check_at <= $1
	-- manifests without any check first, then sorted by schedule, then sorted by digest for deterministic behavior in unit test
	ORDER BY next_check_at IS NULL DESC, next_check_at ASC, digest ASC
	-- only one manifests at a time
	LIMIT %d
	-- prevent other job loops from working on the same asset concurrently
	FOR UPDATE SKIP LOCKED
`, trivySecurityInfoBatchSize))

func (j *Janitor) CheckTrivySecurityStatusJob(registerer prometheus.Registerer) jobloop.Job {
	return (&jobloop.TxGuardedJob[*gorp.Transaction, []keppel.TrivySecurityInfo]{
		Metadata: jobloop.JobMetadata{
			ReadableName:    "check trivy security status",
			ConcurrencySafe: true,
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_trivy_security_status_checks",
				Help: "Counter for Trivy security checks runs in manifests.",
			},
		},
		BeginTx: j.db.Begin,
		DiscoverRow: func(_ context.Context, tx *gorp.Transaction, _ prometheus.Labels) (securityInfos []keppel.TrivySecurityInfo, err error) {
			_, err = tx.Select(&securityInfos, securityCheckSelectQuery, j.timeNow())

			// jobloop expects to receive errNoRows instead of an empty result
			if len(securityInfos) == 0 {
				err = sql.ErrNoRows
			}

			return securityInfos, err
		},
		ProcessRow: j.processTrivySecurityInfo,
	}).Setup(registerer)
}

// processTrivySecurityInfo parallelises the CheckTrivySecurityStatusJob jobloop without requiring extra database connections.
// It processes SecurityInfos in batches maximum the size of trivySecurityInfoBatchSize and runs the value of trivySecurityInfoThreads in parallel.
func (j *Janitor) processTrivySecurityInfo(ctx context.Context, tx *gorp.Transaction, securityInfos []keppel.TrivySecurityInfo, labels prometheus.Labels) error {
	// prevent deadlocks by waiting for more securityInfos in range below
	batchSize := trivySecurityInfoBatchSize
	lenSecurityInfo := len(securityInfos)
	if batchSize < lenSecurityInfo {
		batchSize = lenSecurityInfo
	}

	inputChan := make(chan keppel.TrivySecurityInfo, batchSize)
	for _, securityInfo := range securityInfos {
		inputChan <- securityInfo
	}
	close(inputChan)

	threads := trivySecurityInfoThreads
	// don't start more threads than we possible can saturate
	if threads < lenSecurityInfo {
		threads = lenSecurityInfo
	}

	type chanReturnStruct struct {
		securityInfo keppel.TrivySecurityInfo
		err          error
	}

	// create a channel the size of the threads we are going to spawn to not deadlock when ranging over it
	returnChan := make(chan chanReturnStruct, threads)
	// The WaitGroup keeps track of the opened go routines and makes sure the returnChan is closed when all started go routines exited.
	var wg sync.WaitGroup

	for i := 0; i < threads; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			// inputChan acts as a queue here and each go routine picks the next SecurityInfo task when it is done with the previous
			for securityInfo := range inputChan {
				err := j.doSecurityCheck(ctx, &securityInfo)
				returnChan <- chanReturnStruct{
					securityInfo: securityInfo,
					err:          err,
				}
			}
		}()
	}

	// make sure the below range over the returnChan is not blocking forever
	go func() {
		wg.Wait()
		close(returnChan)
	}()

	var errs errext.ErrorSet
	for returned := range returnChan {
		if returned.err != nil {
			errs.Add(returned.err)
		}

		_, err := tx.Update(&returned.securityInfo)
		errs.Add(err)
	}

	errs.Add(tx.Commit())

	if !errs.IsEmpty() {
		return errors.New(errs.Join(", "))
	}

	return nil
}

var securityInfoCheckSubmanifestInfoQuery = sqlext.SimplifyWhitespace(`
	SELECT t.vuln_status FROM manifests m
	JOIN manifest_manifest_refs r ON m.digest = r.child_digest
	JOIN trivy_security_info t ON m.digest = t.digest
		WHERE r.parent_digest = $1
`)

var trivyTransientErrorsRxs = []*regexp.Regexp{
	regexp.MustCompile(`connect: connection refused$`),
	regexp.MustCompile(`i/o timeout$`),
	regexp.MustCompile(`unexpected status code 502 Bad Gateway$`),
	regexp.MustCompile(`unexpected status code 503 Service Unavailable$`),
}

func isTrivyTransientError(msg string) bool {
	for _, rx := range trivyTransientErrorsRxs {
		if rx.MatchString(msg) {
			return true
		}
	}
	return false
}

func (j *Janitor) doSecurityCheck(ctx context.Context, securityInfo *keppel.TrivySecurityInfo) (returnedError error) {
	// load corresponding repo, account and manifest
	repo, err := keppel.FindRepositoryByID(j.db, securityInfo.RepositoryID)
	if err != nil {
		return fmt.Errorf("cannot find repo for manifest %s: %w", securityInfo.Digest, err)
	}
	account, err := keppel.FindAccount(j.db, repo.AccountName)
	if err != nil {
		return fmt.Errorf("cannot find account for repo %s: %w", repo.FullName(), err)
	}
	manifest, err := keppel.FindManifest(j.db, *repo, securityInfo.Digest)
	if err != nil {
		return fmt.Errorf("cannot find manifest for repo %s and digest %s: %w", repo.FullName(), securityInfo.Digest, err)
	}

	//clear timing information (this will be filled down below once we actually talk to Trivy;
	//if any preflight check fails, the fields stay at nil)
	securityInfo.CheckedAt = nil
	securityInfo.CheckDurationSecs = nil

	//skip validation while account is in maintenance (maintenance mode blocks
	//all kinds of activity on an account's contents)
	if account.InMaintenance {
		securityInfo.NextCheckAt = j.timeNow().Add(j.addJitter(1 * time.Hour))
		return nil
	}

	continueCheck, layerBlobs, err := j.checkPreConditionsForTrivy(ctx, *account, *repo, *manifest, securityInfo)
	if err != nil {
		return err
	}
	if !continueCheck {
		return nil
	}

	relevantPolicies, err := account.SecurityScanPoliciesFor(*repo)
	if err != nil {
		return err
	}

	//we know that this image will not be "Unsupported", so the rest is the part where we actually
	//talk to Trivy (well, mostly anyway), so that part deserves to be measured for performance
	checkStartedAt := j.timeNow()
	defer func() {
		if returnedError == nil {
			checkFinishedAt := j.timeNow()
			securityInfo.CheckedAt = &checkFinishedAt
			duration := checkFinishedAt.Sub(checkStartedAt).Seconds()
			securityInfo.CheckDurationSecs = &duration
			return
		}

		// retry in a bit again but only write down the error if it is not transient
		securityInfo.NextCheckAt = j.timeNow().Add(j.addJitter(5 * time.Minute))

		if !isTrivyTransientError(returnedError.Error()) {
			securityInfo.Message = returnedError.Error()
			securityInfo.VulnerabilityStatus = trivy.ErrorVulnerabilityStatus
		}

		returnedError = fmt.Errorf("cannot check manifest %s@%s: %w", repo.FullName(), securityInfo.Digest, returnedError)
	}()

	imageRef := models.ImageReference{
		Host:      j.cfg.APIPublicHostname,
		RepoName:  fmt.Sprintf("%s/%s", account.Name, repo.Name),
		Reference: models.ManifestReference{Digest: manifest.Digest},
	}

	tokenResp, err := auth.IssueTokenForTrivy(j.cfg, repo.FullName())
	if err != nil {
		return err
	}

	//ask Trivy for the security status of the manifest
	securityInfo.Message = "" //unless it gets set to something else below

	// Trivy has an internal timeout we set to 10m per image (which is already an
	// insanely generous timeout) and we give it a bit of headroom to start
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute+30*time.Second)
	defer cancel()

	var securityStatuses []trivy.VulnerabilityStatus

	if len(layerBlobs) > 0 {
		parsedTrivyReport, err := j.cfg.Trivy.ScanManifestAndParse(ctx, tokenResp.Token, imageRef)
		if err != nil {
			return fmt.Errorf("scan error: %w", err)
		}

		if parsedTrivyReport.Metadata.OS != nil && parsedTrivyReport.Metadata.OS.Eosl {
			securityStatuses = append(securityStatuses, trivy.RottenVulnerabilityStatus)
		}
		for _, result := range parsedTrivyReport.Results {
			for _, vuln := range result.Vulnerabilities {
				securityStatus, ok := trivy.MapToTrivySeverity[vuln.Severity]
				if !ok {
					return fmt.Errorf("vulnerability severity with name %s returned from trivy is unknown and cannot be mapped", securityStatus)
				}

				policy := relevantPolicies.PolicyForVulnerability(vuln)
				if policy != nil {
					securityStatus = policy.VulnerabilityStatus()
				}
				securityStatuses = append(securityStatuses, securityStatus)
			}
		}
	}

	//collect vulnerability status of constituent images
	err = sqlext.ForeachRow(j.db, securityInfoCheckSubmanifestInfoQuery, []interface{}{manifest.Digest}, func(rows *sql.Rows) error {
		var vulnStatus trivy.VulnerabilityStatus
		err := rows.Scan(&vulnStatus)
		securityStatuses = append(securityStatuses, vulnStatus)
		return err
	})
	if err != nil {
		return err
	}

	//merge all vulnerability statuses
	securityInfo.VulnerabilityStatus = trivy.MergeVulnerabilityStatuses(securityStatuses...)
	//regular recheck loop (vulnerability status might change if Trivy adds new vulnerabilities to its DB)
	securityInfo.NextCheckAt = j.timeNow().Add(j.addJitter(1 * time.Hour))

	return nil
}

var blobUncompressedSizeTooBigGiB float64 = 10

func (j *Janitor) checkPreConditionsForTrivy(ctx context.Context, account keppel.Account, repo keppel.Repository, manifest keppel.Manifest, securityInfo *keppel.TrivySecurityInfo) (continueCheck bool, layerBlobs []keppel.Blob, err error) {
	layerBlobs, err = j.collectManifestLayerBlobs(ctx, account, repo, manifest)
	if err != nil {
		return false, nil, err
	}

	// filter media types that trivy is known to support
	for _, blob := range layerBlobs {
		if blob.MediaType == schema2.MediaTypeLayer || blob.MediaType == imageSpecs.MediaTypeImageLayerGzip {
			continue
		}

		securityInfo.VulnerabilityStatus = trivy.UnsupportedVulnerabilityStatus
		securityInfo.Message = fmt.Sprintf("vulnerability scanning is not supported for blob layers with media type %q", blob.MediaType)
		securityInfo.NextCheckAt = j.timeNow().Add(j.addJitter(24 * time.Hour))
		return false, layerBlobs, nil
	}

	//can only validate when all blobs are present in the storage
	for _, blob := range layerBlobs {
		if blob.StorageID == "" {
			//if the manifest is fairly new, the user who replicated it is probably
			//still replicating it; give them 10 minutes to finish replicating it
			securityInfo.NextCheckAt = manifest.PushedAt.Add(j.addJitter(10 * time.Minute))
			if securityInfo.NextCheckAt.After(j.timeNow()) {
				return false, layerBlobs, nil
			}
			//otherwise we do the replication ourselves
			_, err := j.processor().ReplicateBlob(ctx, blob, account, repo, nil)
			if err != nil {
				return false, layerBlobs, err
			}
			//after successful replication, restart this call to read the new blob with the correct StorageID from the DB
			return j.checkPreConditionsForTrivy(ctx, account, repo, manifest, securityInfo)
		}

		if blob.BlocksVulnScanning == nil && strings.HasSuffix(blob.MediaType, "gzip") {
			//uncompress the blob to check if it's too large for Trivy to handle within its allotted timeout
			reader, _, err := j.sd.ReadBlob(account, blob.StorageID)
			if err != nil {
				return false, layerBlobs, fmt.Errorf("cannot read blob %s: %w", blob.Digest, err)
			}
			defer reader.Close()
			gzipReader, err := gzip.NewReader(reader)
			if err != nil {
				return false, layerBlobs, fmt.Errorf("cannot unzip blob %s: %w", blob.Digest, err)
			}
			defer gzipReader.Close()

			//when measuring uncompressed size, use LimitReader as a simple but
			//effective guard against zip bombs
			limitBytes := int64(1 << 30 * blobUncompressedSizeTooBigGiB)
			numberBytes, err := io.Copy(io.Discard, io.LimitReader(gzipReader, limitBytes+1))
			if err != nil {
				return false, layerBlobs, fmt.Errorf("cannot unzip blob %s: %w", blob.Digest, err)
			}

			// mark blocked for vulnerability scanning if one layer/blob is bigger than 10 GiB
			blocksVulnScanning := numberBytes >= limitBytes
			blob.BlocksVulnScanning = &blocksVulnScanning
			_, err = j.db.WithContext(ctx).Exec(`UPDATE blobs SET blocks_vuln_scanning = $1 WHERE id = $2`, blocksVulnScanning, blob.ID)
			if err != nil {
				return false, layerBlobs, err
			}
		}

		if blob.BlocksVulnScanning != nil && *blob.BlocksVulnScanning {
			securityInfo.VulnerabilityStatus = trivy.UnsupportedVulnerabilityStatus
			securityInfo.Message = fmt.Sprintf("vulnerability scanning is not supported for uncompressed image layers above %g GiB", blobUncompressedSizeTooBigGiB)
			securityInfo.NextCheckAt = j.timeNow().Add(j.addJitter(24 * time.Hour))
			return false, layerBlobs, nil
		}
	}

	return true, layerBlobs, nil
}
