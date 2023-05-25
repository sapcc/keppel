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
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/distribution/manifest/schema2"
	"github.com/go-gorp/gorp/v3"
	"github.com/opencontainers/go-digest"
	imageSpecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/jobloop"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sqlext"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/clair"
	peerclient "github.com/sapcc/keppel/internal/client/peer"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/processor"
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

// ValidateNextManifest validates manifests that have not been validated for more
// than 6 hours. At most one manifest is validated per call. If no manifest
// needs to be validated, sql.ErrNoRows is returned.
func (j *Janitor) ValidateNextManifest() (returnErr error) {
	var manifest keppel.Manifest

	defer func() {
		if returnErr == nil {
			validateManifestSuccessCounter.Inc()
		} else if returnErr != sql.ErrNoRows {
			validateManifestFailedCounter.Inc()
			if manifest.Digest == "" || manifest.RepositoryID == 0 {
				returnErr = fmt.Errorf("while validating a manifest: %w", returnErr)
			} else {
				returnErr = fmt.Errorf("while validating manifest %s in repo %d: %w", manifest.Digest, manifest.RepositoryID, returnErr)
			}
		}
	}()

	//find manifest: validate once every 24 hours, but recheck after 10 minutes if
	//validation failed
	maxSuccessfulValidatedAt := j.timeNow().Add(-24 * time.Hour)
	maxFailedValidatedAt := j.timeNow().Add(-10 * time.Minute)
	err := j.db.SelectOne(&manifest, outdatedManifestSearchQuery, maxSuccessfulValidatedAt, maxFailedValidatedAt)
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
		return fmt.Errorf("cannot find repo %d for manifest %s: %w", manifest.RepositoryID, manifest.Digest, err)
	}
	account, err := keppel.FindAccount(j.db, repo.AccountName)
	if err != nil {
		return fmt.Errorf("cannot find account for manifest %s/%s: %w", repo.FullName(), manifest.Digest, err)
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
			err = fmt.Errorf("%w (additional error encountered while recording validation error: %w)", err, updateErr)
		}
		return err
	}

	return nil
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

// SyncManifestsInNextRepo finds the next repository in a replica account where
// manifests have not been synced for more than an hour, and syncs its manifests.
// Syncing involves checking with the primary account which manifests have been
// deleted there, and replicating the deletions on our side.
//
// If no repo needs syncing, sql.ErrNoRows is returned.
func (j *Janitor) SyncManifestsInNextRepo() (returnErr error) {
	var repo keppel.Repository

	defer func() {
		if returnErr == nil {
			syncManifestsSuccessCounter.Inc()
		} else if returnErr != sql.ErrNoRows {
			syncManifestsFailedCounter.Inc()
			repoFullName := repo.FullName()
			if repoFullName == "" {
				repoFullName = "unknown"
			}
			returnErr = fmt.Errorf("while syncing manifests in the replica repo %s: %w", repoFullName, returnErr)
		}
	}()

	//find repository to sync
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
		return fmt.Errorf("cannot find account for repo %s: %w", repo.FullName(), err)
	}

	//do not perform manifest sync while account is in maintenance (maintenance mode blocks all kinds of replication)
	if !account.InMaintenance {
		syncPayload, err := j.getReplicaSyncPayload(*account, repo)
		if err != nil {
			return err
		}
		err = j.performTagSync(*account, repo, syncPayload)
		if err != nil {
			return err
		}
		err = j.performManifestSync(*account, repo, syncPayload)
		if err != nil {
			return err
		}
	}

	_, err = j.db.Exec(syncManifestDoneQuery, repo.ID, j.timeNow().Add(j.addJitter(1*time.Hour)))
	if err != nil {
		return err
	}
	_, err = j.db.Exec(syncManifestCleanupEmptyQuery, repo.ID)
	return err
}

// When performing a manifest/tag sync, and the upstream is one of our peers,
// we can use the replica-sync API instead of polling each manifest and tag
// individually. This also synchronizes our own last_pulled_at timestamps into
// the primary account. The primary therefore gains a complete picture of pull
// activity, which is required for some GC policies to work correctly.
func (j *Janitor) getReplicaSyncPayload(account keppel.Account, repo keppel.Repository) (*keppel.ReplicaSyncPayload, error) {
	//the replica-sync API is only available when upstream is a peer
	if account.UpstreamPeerHostName == "" {
		return nil, nil
	}

	//get peer
	var peer keppel.Peer
	err := j.db.SelectOne(&peer, `SELECT * FROM peers WHERE hostname = $1`, account.UpstreamPeerHostName)
	if err != nil {
		return nil, err
	}

	//get token for peer
	client, err := peerclient.New(j.cfg, peer, auth.PeerAPIScope)
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

	return client.PerformReplicaSync(repo.FullName(), keppel.ReplicaSyncPayload{Manifests: manifests})
}

func (j *Janitor) performTagSync(account keppel.Account, repo keppel.Repository, syncPayload *keppel.ReplicaSyncPayload) error {
	var tags []keppel.Tag
	_, err := j.db.Select(&tags, `SELECT * FROM tags WHERE repo_id = $1`, repo.ID)
	if err != nil {
		return fmt.Errorf("cannot list tags in repo %s: %w", repo.FullName(), err)
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
				_, err := j.db.Delete(&tag) //nolint:gosec // Delete is not holding onto the pointer after it returns
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
		ref := keppel.ManifestReference{Tag: tag.Name}
		_, _, err := p.ReplicateManifest(account, repo, ref, keppel.AuditContext{
			UserIdentity: janitorUserIdentity{TaskName: "tag-sync"},
			Request:      janitorDummyRequest,
		})
		if err != nil {
			//if the tag itself (and only the tag itself!) 404s, we can replicate the
			//tag deletion into our replica
			err404, ok := err.(processor.UpstreamManifestMissingError)
			if ok && err404.Ref == ref {
				_, err := j.db.Delete(&tag) //nolint:gosec // Delete is not holding onto the pointer after it returns
				if err != nil {
					return err
				}
			} else {
				//all other errors fail the sync
				return err
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

func (j *Janitor) performManifestSync(account keppel.Account, repo keppel.Repository, syncPayload *keppel.ReplicaSyncPayload) error {
	//enumerate manifests in this repo (this only needs to consider untagged
	//manifests: we run right after performTagSync, therefore all images that are
	//tagged right now were already confirmed to still be good)
	var manifests []keppel.Manifest
	_, err := j.db.Select(&manifests, repoUntaggedManifestsSelectQuery, repo.ID)
	if err != nil {
		return fmt.Errorf("cannot list manifests in repo %s: %w", repo.FullName(), err)
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
		ref := keppel.ManifestReference{Digest: manifest.Digest}
		exists, err := p.CheckManifestOnPrimary(account, repo, ref)
		if err != nil {
			return fmt.Errorf("cannot check existence of manifest %s/%s on primary account: %w", repo.FullName(), manifest.Digest, err)
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
		return fmt.Errorf("cannot enumerate manifest-manifest refs in repo %s: %w", repo.FullName(), err)
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
			err := j.processor().DeleteManifest(account, repo, digestToBeDeleted, keppel.AuditContext{
				UserIdentity: janitorUserIdentity{TaskName: "manifest-sync"},
				Request:      janitorDummyRequest,
			})
			if err != nil {
				return fmt.Errorf("cannot remove deleted manifest %s in repo %s: %w", digestToBeDeleted, repo.FullName(), err)
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
			return fmt.Errorf("cannot remove deleted manifests %v in repo %s because they are still being referenced by other manifests (this smells like an inconsistency on the primary account)",
				maps.Keys(shallDeleteManifest), repo.FullName())
		}
	}

	return nil
}

var vulnCheckSelectQuery = sqlext.SimplifyWhitespace(`
	SELECT * FROM vuln_info
		WHERE next_check_at <= $1
	-- manifests without any check first, then prefer manifests without a finished check, then sorted by schedule, then sorted by digest for deterministic behavior in unit test
	ORDER BY next_check_at IS NULL DESC, status = 'Pending' DESC, next_check_at ASC, digest ASC
	-- only one manifests at a time
	LIMIT 1
	-- prevent other job loops from working on the same asset concurrently
	FOR UPDATE SKIP LOCKED
`)

var vulnCheckBlobSelectQuery = sqlext.SimplifyWhitespace(`
	SELECT b.* FROM blobs b
	JOIN manifest_blob_refs r ON b.id = r.blob_id
		WHERE r.repo_id = $1 AND r.digest = $2
`)

var vulnCheckSubmanifestInfoQuery = sqlext.SimplifyWhitespace(`
	SELECT v.status FROM manifests m
	JOIN manifest_manifest_refs r ON m.digest = r.child_digest
	JOIN vuln_info v ON m.digest = v.digest
		WHERE r.parent_digest = $1
`)

// CheckVulnerabilitiesForNextManifest finds the next manifest that has not been
// checked for vulnerabilities yet (or within the last hour), and runs the
// vulnerability check by submitting the image to Clair.
//
// This assumes that `j.cfg.Clair != nil`.
//
// If no manifest needs checking, sql.ErrNoRows is returned.
func (j *Janitor) CheckVulnerabilitiesForNextManifest() JobPoller {
	return func() (job Job, returnErr error) {
		defer func() {
			if returnErr == nil {
				checkVulnerabilitySuccessCounter.Inc()
			} else if returnErr != sql.ErrNoRows {
				checkVulnerabilityFailedCounter.Inc()
				returnErr = fmt.Errorf("while updating vulnerability status for a manifest: %w", returnErr)
			}
		}()

		//we need a DB transaction for the row-level locking to work correctly
		tx, err := j.db.Begin()
		if err != nil {
			return nil, err
		}
		defer func() {
			if returnErr != nil {
				sqlext.RollbackUnlessCommitted(tx)
			}
		}()

		//find vulnInfo to sync
		var vulnInfo keppel.VulnerabilityInfo
		err = tx.SelectOne(&vulnInfo, vulnCheckSelectQuery, j.timeNow())
		if err != nil {
			if err == sql.ErrNoRows {
				logg.Debug("no vulnerability to update status for - slowing down...")
				//nolint:errcheck
				tx.Rollback() //avoid the log line generated by sqlext.RollbackUnlessCommitted()
				return nil, sql.ErrNoRows
			}
			return nil, err
		}

		return checkVulnerabilitiesJob{j, tx, vulnInfo}, nil
	}
}

type checkVulnerabilitiesJob struct {
	j        *Janitor
	tx       *gorp.Transaction
	vulnInfo keppel.VulnerabilityInfo
}

func (job checkVulnerabilitiesJob) Execute() (returnError error) {
	j := job.j
	tx := job.tx
	vulnInfo := job.vulnInfo

	defer sqlext.RollbackUnlessCommitted(tx)

	//load corresponding repo, account and manifest
	repo, err := keppel.FindRepositoryByID(tx, vulnInfo.RepositoryID)
	if err != nil {
		return fmt.Errorf("cannot find repo for manifest %s: %w", vulnInfo.Digest, err)
	}
	account, err := keppel.FindAccount(tx, repo.AccountName)
	if err != nil {
		return fmt.Errorf("cannot find account for repo %s: %w", repo.FullName(), err)
	}
	manifest, err := keppel.FindManifest(tx, *repo, vulnInfo.Digest)
	if err != nil {
		return fmt.Errorf("cannot find manifest for repo %s and digest %s: %w", repo.FullName(), vulnInfo.Digest, err)
	}

	err = j.doVulnerabilityCheck(*account, *repo, *manifest, &vulnInfo)
	if err != nil {
		return err
	}
	_, err = tx.Update(&vulnInfo)
	if err != nil {
		return err
	}

	return tx.Commit()
}

var (
	manifestSizeTooBigGiB         float64 = 5
	blobUncompressedSizeTooBigGiB float64 = 10
)

func (j *Janitor) collectManifestReferencedBlobs(account keppel.Account, repo keppel.Repository, manifest keppel.Manifest) (layerBlobs []keppel.Blob, err error) {
	//we need all blobs directly referenced by this manifest (we do not care
	//about submanifests at this level, the reports from those will be merged
	//later on in the API)
	var blobs []keppel.Blob
	_, err = j.db.Select(&blobs, vulnCheckBlobSelectQuery, manifest.RepositoryID, manifest.Digest)
	if err != nil {
		return nil, err
	}

	//the Clair manifest can only include blobs that are actual image layers, so we need to parse the manifest contents
	manifestBytes, err := j.sd.ReadManifest(account, repo.Name, manifest.Digest)
	if err != nil {
		return nil, err
	}
	manifestParsed, manifestDesc, err := keppel.ParseManifest(manifest.MediaType, manifestBytes)
	if err != nil {
		return nil, keppel.ErrManifestInvalid.With(err.Error())
	}
	if manifest.Digest != "" && manifestDesc.Digest != manifest.Digest {
		return nil, keppel.ErrDigestInvalid.With("actual manifest digest is " + manifestDesc.Digest.String())
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

func (j *Janitor) checkPreConditionsForClair(account keppel.Account, repo keppel.Repository, manifest keppel.Manifest, vulnInfo *keppel.VulnerabilityInfo) (layerBlobs []keppel.Blob, ok bool, err error) {
	//NOTE: On success, `layerBlobs` is returned to the caller because doVulnerabilityCheck() also needs this list.
	//
	//We used to pre-compute `layerBlobs` before calling this function, but this
	//does not work because we want to restart this call after being done with
	//blob replication. The new call needs to see the updated blobs list,
	//otherwise it will try to replicate the same blobs again and end up in an
	//endless loop.
	layerBlobs, err = j.collectManifestReferencedBlobs(account, repo, manifest)
	if err != nil {
		return nil, false, err
	}

	// filter media types that clair is known to support
	for _, blob := range layerBlobs {
		if blob.MediaType == schema2.MediaTypeLayer || blob.MediaType == imageSpecs.MediaTypeImageLayerGzip {
			continue
		}

		vulnInfo.Status = clair.UnsupportedVulnerabilityStatus
		vulnInfo.Message = fmt.Sprintf("vulnerability scanning is not supported for blob layers with media type %q", blob.MediaType)
		vulnInfo.NextCheckAt = j.timeNow().Add(j.addJitter(24 * time.Hour))
		return nil, false, nil
	}

	//skip when blobs add up to more than 5 GiB
	if manifest.SizeBytes >= uint64(1<<30*manifestSizeTooBigGiB) {
		vulnInfo.Status = clair.UnsupportedVulnerabilityStatus
		vulnInfo.Message = fmt.Sprintf("vulnerability scanning is not supported for images above %g GiB", manifestSizeTooBigGiB)
		vulnInfo.NextCheckAt = j.timeNow().Add(j.addJitter(24 * time.Hour))
		return nil, false, nil
	}

	//can only validate when all blobs are present in the storage
	for _, blob := range layerBlobs {
		if blob.StorageID == "" {
			//if the manifest is fairly new, the user who replicated it is probably
			//still replicating it; give them 10 minutes to finish replicating it
			vulnInfo.NextCheckAt = manifest.PushedAt.Add(j.addJitter(10 * time.Minute))
			if vulnInfo.NextCheckAt.After(j.timeNow()) {
				return nil, false, nil
			}
			//otherwise we do the replication ourselves
			_, err := j.processor().ReplicateBlob(blob, account, repo, nil)
			if err != nil {
				return nil, false, err
			}
			//after successful replication, restart this call to read the new blob with the correct StorageID from the DB
			return j.checkPreConditionsForClair(account, repo, manifest, vulnInfo)
		}

		if blob.BlocksVulnScanning == nil && strings.HasSuffix(blob.MediaType, "gzip") {
			//uncompress the blob to check if it's too large for Clair to handle
			reader, _, err := j.sd.ReadBlob(account, blob.StorageID)
			if err != nil {
				return nil, false, err
			}
			defer reader.Close()
			gzipReader, err := gzip.NewReader(reader)
			if err != nil {
				return nil, false, err
			}
			defer gzipReader.Close()

			//when measuring uncompressed size, use LimitReader as a simple but
			//effective guard against zip bombs
			limitBytes := int64(1 << 30 * blobUncompressedSizeTooBigGiB)
			numberBytes, err := io.Copy(io.Discard, io.LimitReader(gzipReader, limitBytes+1))
			if err != nil {
				return nil, false, err
			}

			// mark blocked for vulnerability scanning if one layer/blob is bigger than 10 GiB
			blocksVulnScanning := numberBytes >= limitBytes
			blob.BlocksVulnScanning = &blocksVulnScanning
			_, err = j.db.Exec(`UPDATE blobs SET blocks_vuln_scanning = $1 WHERE id = $2`, blocksVulnScanning, blob.ID)
			if err != nil {
				return nil, false, err
			}
		}

		if blob.BlocksVulnScanning != nil && *blob.BlocksVulnScanning {
			vulnInfo.Status = clair.UnsupportedVulnerabilityStatus
			vulnInfo.Message = fmt.Sprintf("vulnerability scanning is not supported for uncompressed image layers above %g GiB", blobUncompressedSizeTooBigGiB)
			vulnInfo.NextCheckAt = j.timeNow().Add(j.addJitter(24 * time.Hour))
			return nil, false, nil
		}
	}

	return layerBlobs, true, nil
}

func (j *Janitor) doVulnerabilityCheck(account keppel.Account, repo keppel.Repository, manifest keppel.Manifest, vulnInfo *keppel.VulnerabilityInfo) (returnedError error) {
	//clear timing information (this will be filled down below once we actually talk to Clair;
	//if any preflight check fails, the fields stay at nil)
	vulnInfo.CheckedAt = nil
	vulnInfo.CheckDurationSecs = nil

	//skip validation while account is in maintenance (maintenance mode blocks
	//all kinds of activity on an account's contents)
	if account.InMaintenance {
		vulnInfo.NextCheckAt = j.timeNow().Add(j.addJitter(1 * time.Hour))
		return nil
	}

	layerBlobs, continueCheck, err := j.checkPreConditionsForClair(account, repo, manifest, vulnInfo)
	if err != nil {
		return err
	}
	if !continueCheck {
		return nil
	}

	//we know that this image will not be "Unsupported", so the rest is the part where we actually
	//talk to Clair (well, mostly anyway), so that part deserves to be measured for performance
	checkStartedAt := j.timeNow()
	defer func() {
		if returnedError == nil {
			checkFinishedAt := j.timeNow()
			vulnInfo.CheckedAt = &checkFinishedAt
			duration := checkFinishedAt.Sub(checkStartedAt).Seconds()
			vulnInfo.CheckDurationSecs = &duration
		}
	}()
	//also we don't allow Clair to take more than 10 minutes on a single image (which is already an
	//insanely generous timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	//collect vulnerability status of constituent images
	var vulnStatuses []clair.VulnerabilityStatus
	err = sqlext.ForeachRow(j.db, vulnCheckSubmanifestInfoQuery, []interface{}{manifest.Digest}, func(rows *sql.Rows) error {
		var vulnStatus clair.VulnerabilityStatus
		err := rows.Scan(&vulnStatus)
		vulnStatuses = append(vulnStatuses, vulnStatus)
		return err
	})
	if err != nil {
		return err
	}

	//ask Clair for vulnerability status of blobs in this image
	vulnInfo.Message = "" //unless it gets set to something else below
	if len(layerBlobs) > 0 {
		clairState, err := j.cfg.ClairClient.CheckManifestState(ctx, manifest.Digest, func() (clair.Manifest, error) {
			return j.buildClairManifest(account, manifest, layerBlobs)
		})
		if err != nil {
			return err
		}
		now := j.timeNow()
		if vulnInfo.IndexStartedAt == nil {
			vulnInfo.IndexStartedAt = &now
			vulnInfo.IndexState = clairState.IndexState
		}

		if clairState.IndexingWasRestarted {
			vulnStatuses = append(vulnStatuses, clair.PendingVulnerabilityStatus)
			vulnInfo.IndexStartedAt = &now
			vulnInfo.IndexState = clairState.IndexState
			checkVulnerabilityRetriedCounter.Inc()
		} else if clairState.IsErrored {
			vulnStatuses = append(vulnStatuses, clair.ErrorVulnerabilityStatus)
			vulnInfo.Message = clairState.ErrorMessage
		} else if clairState.IsIndexed {
			if vulnInfo.IndexFinishedAt == nil {
				vulnInfo.IndexFinishedAt = &now
			}

			clairReport, err := j.cfg.ClairClient.GetVulnerabilityReport(ctx, manifest.Digest)
			if err != nil {
				return err
			}
			if clairReport == nil {
				//nolint:stylecheck // Clair is a proper name
				return fmt.Errorf("Clair reports indexing of %s as finished, but vulnerability report is 404", manifest.Digest)
			}
			vulnStatuses = append(vulnStatuses, clairReport.VulnerabilityStatus())
		} else {
			vulnStatuses = append(vulnStatuses, clair.PendingVulnerabilityStatus)
		}
	}

	//merge all vulnerability statuses
	vulnInfo.Status = clair.MergeVulnerabilityStatuses(vulnStatuses...)
	if vulnInfo.Status == clair.PendingVulnerabilityStatus {
		logg.Info("skipping vulnerability check for %s: indexing is not finished yet", manifest.Digest)
		//wait a bit for indexing to finish, then come back to update the vulnerability status
		vulnInfo.NextCheckAt = j.timeNow().Add(j.addJitter(2 * time.Minute))
	} else {
		//regular recheck loop (vulnerability status might change if Clair adds new vulnerabilities to its DB)
		vulnInfo.NextCheckAt = j.timeNow().Add(j.addJitter(1 * time.Hour))
	}
	return nil
}

func (j *Janitor) buildClairManifest(account keppel.Account, manifest keppel.Manifest, layerBlobs []keppel.Blob) (clair.Manifest, error) {
	result := clair.Manifest{
		Digest: manifest.Digest,
	}

	for _, blob := range layerBlobs {
		blobURL, err := j.sd.URLForBlob(account, blob.StorageID)
		//TODO handle ErrCannotGenerateURL (currently not a problem because all storage drivers can make URLs)
		if err != nil {
			return clair.Manifest{}, err
		}
		result.Layers = append(result.Layers, clair.Layer{
			Digest: blob.Digest,
			URL:    blobURL,
		})
	}

	return result, nil
}

var getDigestForIndexStatesToResubmitQuery = sqlext.SimplifyWhitespace(fmt.Sprintf(`
	SELECT digest from vuln_info
	WHERE index_finished_at IS NOT NULL
		AND next_check_at > $1 -- do not delete index reports that the vulnerability check loop is currently inspecting or about to inspect
		AND index_state != $2
		AND status != '%s'
	LIMIT $3
`, clair.PendingVulnerabilityStatus))

// TODO: remove when clair is removed
func (j *Janitor) CheckClairManifestState() error {
	//limit the total runtime of this task
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	indexStateHash, err := j.cfg.ClairClient.GetIndexStateHash(ctx)
	if err != nil {
		return err
	}

	var total, inPending int64
	query := fmt.Sprintf("SELECT COUNT(*), COUNT(CASE WHEN status = '%s' THEN TRUE ELSE NULL END) FROM vuln_info", clair.PendingVulnerabilityStatus)
	err = j.db.QueryRow(query).Scan(&total, &inPending)
	if err != nil {
		return err
	}

	// only schedule up to 1% or at minimum 10
	concurrent := (total / 100)
	if concurrent < 10 {
		concurrent = 10
	}

	scheduleNew := concurrent - inPending

	// if nothing new can be scheduled, wait and exit early
	if scheduleNew <= 0 {
		return nil
	}

	err = sqlext.ForeachRow(j.db, getDigestForIndexStatesToResubmitQuery, []any{j.timeNow().Add(1 * time.Minute), indexStateHash, scheduleNew},
		func(rows *sql.Rows) error {
			var digest digest.Digest
			err := rows.Scan(&digest)
			if err != nil {
				return err
			}

			err = j.setManifestAndParentsToPending(ctx, digest)
			return err
		},
	)
	return err
}

func (j *Janitor) setManifestAndParentsToPending(ctx context.Context, manifestDigest digest.Digest) error {
	err := j.cfg.ClairClient.DeleteManifest(ctx, manifestDigest)
	if err != nil {
		return err
	}

	_, err = j.db.Exec(sqlext.SimplifyWhitespace(`
		UPDATE vuln_info SET status = $1, index_state = '', next_check_at = $2
		WHERE digest = $3 OR digest IN (
			SELECT parent_digest FROM manifest_manifest_refs WHERE child_digest = $3
		)`),
		clair.PendingVulnerabilityStatus, j.timeNow(), manifestDigest)

	return err
}

var securityCheckSelectQuery = sqlext.SimplifyWhitespace(`
	SELECT * FROM trivy_security_info
	WHERE next_check_at <= $1
	-- manifests without any check first, then sorted by schedule, then sorted by digest for deterministic behavior in unit test
	ORDER BY next_check_at IS NULL DESC, next_check_at ASC, digest ASC
	-- only one manifests at a time
	LIMIT 1
	-- prevent other job loops from working on the same asset concurrently
	FOR UPDATE SKIP LOCKED
`)

func (j *Janitor) CheckTrivySecurityStatus(registerer prometheus.Registerer) jobloop.Job {
	return (&jobloop.TxGuardedJob[*gorp.Transaction, keppel.TrivySecurityInfo]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "check trivy security status",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_trivy_security_status_checks",
				Help: "Counter for Trivy security checks runs in manifests.",
			},
		},
		BeginTx: j.db.Begin,
		DiscoverRow: func(_ context.Context, tx *gorp.Transaction, _ prometheus.Labels) (securityInfo keppel.TrivySecurityInfo, err error) {
			err = tx.SelectOne(&securityInfo, securityCheckSelectQuery, j.timeNow())
			return securityInfo, err
		},
		ProcessRow: j.processTrivySecurityInfo,
	}).Setup(registerer)
}

func (j *Janitor) processTrivySecurityInfo(_ context.Context, tx *gorp.Transaction, securityInfo keppel.TrivySecurityInfo, labels prometheus.Labels) error {
	//load corresponding repo, account and manifest
	repo, err := keppel.FindRepositoryByID(tx, securityInfo.RepositoryID)
	if err != nil {
		return fmt.Errorf("cannot find repo for manifest %s: %w", securityInfo.Digest, err)
	}
	account, err := keppel.FindAccount(tx, repo.AccountName)
	if err != nil {
		return fmt.Errorf("cannot find account for repo %s: %w", repo.FullName(), err)
	}
	manifest, err := keppel.FindManifest(tx, *repo, securityInfo.Digest)
	if err != nil {
		return fmt.Errorf("cannot find manifest for repo %s and digest %s: %w", repo.FullName(), securityInfo.Digest, err)
	}

	err = j.doSecurityCheck(*account, *repo, *manifest, &securityInfo)
	if err != nil {
		return err
	}
	_, err = tx.Update(&securityInfo)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (j *Janitor) doSecurityCheck(account keppel.Account, repo keppel.Repository, manifest keppel.Manifest, securityInfo *keppel.TrivySecurityInfo) (returnedError error) {
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

	continueCheck, err := j.checkPreConditionsForTrivy(account, repo, manifest, securityInfo)
	if err != nil {
		return err
	}
	if !continueCheck {
		return nil
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
		} else {
			securityInfo.Message = err.Error()
			securityInfo.NextCheckAt = j.timeNow().Add(j.addJitter(5 * time.Minute))
			securityInfo.VulnerabilityStatus = clair.ErrorVulnerabilityStatus
		}
	}()

	imageRef := keppel.ImageReference{
		Host:      j.cfg.APIPublicHostname,
		RepoName:  fmt.Sprintf("%s/%s", account.Name, repo.Name),
		Reference: keppel.ManifestReference{Digest: manifest.Digest},
	}

	tokenResp, err := auth.Authorization{
		UserIdentity: auth.AnonymousUserIdentity,
		Audience:     auth.Audience{},
		ScopeSet: auth.NewScopeSet(auth.Scope{
			ResourceType: "repository",
			ResourceName: repo.FullName(),
			Actions:      []string{"pull"},
		}),
	}.IssueTokenWithExpires(j.cfg, 20*time.Minute)
	if err != nil {
		return err
	}

	//ask Trivy for the security status of the manifest
	securityInfo.Message = "" //unless it gets set to something else below
	trivyReport, err := j.cfg.Trivy.ScanManifest(imageRef.String(), tokenResp.Token)
	if err != nil {
		return err
	}

	var securityStatuses []clair.VulnerabilityStatus
	for _, result := range trivyReport.Results {
		for _, vulnerability := range result.Vulnerabilities {
			if securityStatus, ok := clair.MapToTrivySeverity[vulnerability.Severity]; ok {
				securityStatuses = append(securityStatuses, securityStatus)
			} else {
				return fmt.Errorf("vulnerability severity with name %s returned from trivy is unknown and cannot be mapped", securityStatus)
			}
		}
	}

	//merge all vulnerability statuses
	securityInfo.VulnerabilityStatus = clair.MergeVulnerabilityStatuses(securityStatuses...)
	//regular recheck loop (vulnerability status might change if Clair adds new vulnerabilities to its DB)
	securityInfo.NextCheckAt = j.timeNow().Add(j.addJitter(1 * time.Hour))

	return nil
}

func (j *Janitor) checkPreConditionsForTrivy(account keppel.Account, repo keppel.Repository, manifest keppel.Manifest, securityInfo *keppel.TrivySecurityInfo) (continueCheck bool, err error) {
	layerBlobs, err := j.collectManifestReferencedBlobs(account, repo, manifest)
	if err != nil {
		return false, err
	}

	// filter media types that trivy is known to support
	for _, blob := range layerBlobs {
		if blob.MediaType == schema2.MediaTypeLayer || blob.MediaType == imageSpecs.MediaTypeImageLayerGzip {
			continue
		}

		securityInfo.VulnerabilityStatus = clair.UnsupportedVulnerabilityStatus
		securityInfo.Message = fmt.Sprintf("vulnerability scanning is not supported for blob layers with media type %q", blob.MediaType)
		securityInfo.NextCheckAt = j.timeNow().Add(j.addJitter(24 * time.Hour))
		return false, nil
	}

	//can only validate when all blobs are present in the storage
	for _, blob := range layerBlobs {
		if blob.StorageID == "" {
			//if the manifest is fairly new, the user who replicated it is probably
			//still replicating it; give them 10 minutes to finish replicating it
			securityInfo.NextCheckAt = manifest.PushedAt.Add(j.addJitter(10 * time.Minute))
			if securityInfo.NextCheckAt.After(j.timeNow()) {
				return false, nil
			}
			//otherwise we do the replication ourselves
			_, err := j.processor().ReplicateBlob(blob, account, repo, nil)
			if err != nil {
				return false, err
			}
			//after successful replication, restart this call to read the new blob with the correct StorageID from the DB
			return j.checkPreConditionsForTrivy(account, repo, manifest, securityInfo)
		}
	}

	return true, nil
}
