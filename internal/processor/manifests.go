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

package processor

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/docker/distribution/manifest/manifestlist"
	"github.com/opencontainers/go-digest"
	imagespec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sqlext"
	"gopkg.in/gorp.v2"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/clair"
	"github.com/sapcc/keppel/internal/client"
	"github.com/sapcc/keppel/internal/keppel"
)

// IncomingManifest contains information about a manifest uploaded by the user
// (or downloaded from a peer registry in the case of replication).
type IncomingManifest struct {
	Reference keppel.ManifestReference
	MediaType string
	Contents  []byte
	PushedAt  time.Time //usually time.Now(), but can be different in unit tests
}

var checkManifestExistsQuery = sqlext.SimplifyWhitespace(`
	SELECT COUNT(*) > 0 FROM manifests WHERE repo_id = $1 AND digest = $2
`)
var checkTagExistsAtSameDigestQuery = sqlext.SimplifyWhitespace(`
	SELECT COUNT(*) > 0 FROM tags WHERE repo_id = $1 AND name = $2 AND digest = $3
`)

// ValidateAndStoreManifest validates the given manifest and stores it under the
// given reference. If the reference is a digest, it is validated. Otherwise, a
// tag with that name is created that points to the new manifest.
func (p *Processor) ValidateAndStoreManifest(account keppel.Account, repo keppel.Repository, m IncomingManifest, actx keppel.AuditContext) (*keppel.Manifest, error) {
	//check if the objects we want to create already exist in the database; this
	//check is not 100% reliable since it does not run in the same transaction as
	//the actual upsert, so results should be taken with a grain of salt; but the
	//result is accurate enough to avoid most duplicate audit events
	contentsDigest := digest.Canonical.FromBytes(m.Contents)
	manifestExistsAlready, err := p.db.SelectBool(checkManifestExistsQuery, repo.ID, contentsDigest.String())
	if err != nil {
		return nil, err
	}
	logg.Debug("ValidateAndStoreManifest: in repo %d, manifest %s already exists = %t", repo.ID, contentsDigest.String(), manifestExistsAlready)
	var tagExistsAlready bool
	if m.Reference.IsTag() {
		tagExistsAlready, err = p.db.SelectBool(checkTagExistsAtSameDigestQuery, repo.ID, m.Reference.Tag, contentsDigest.String())
		if err != nil {
			return nil, err
		}
		logg.Debug("ValidateAndStoreManifest: in repo %d, tag %s @%s already exists = %t", repo.ID, m.Reference.Tag, contentsDigest.String(), tagExistsAlready)
	}

	//the quota check can be skipped if we are sure that we won't need to insert
	//a new row into the manifests table
	if !manifestExistsAlready {
		err = p.checkQuotaForManifestPush(account)
		if err != nil {
			return nil, err
		}
	}

	manifest := &keppel.Manifest{
		//NOTE: .Digest and .SizeBytes are computed by validateAndStoreManifestCommon()
		RepositoryID: repo.ID,
		MediaType:    m.MediaType,
		PushedAt:     m.PushedAt,
		ValidatedAt:  m.PushedAt,
	}
	if m.Reference.IsDigest() {
		//allow validateAndStoreManifestCommon() to validate the user-supplied
		//digest against the actual manifest data
		manifest.Digest = m.Reference.Digest.String()
	}
	err = p.validateAndStoreManifestCommon(account, repo, manifest, m.Contents,
		func(tx *gorp.Transaction) error {
			if m.Reference.IsTag() {
				err = upsertTag(tx, keppel.Tag{
					RepositoryID: repo.ID,
					Name:         m.Reference.Tag,
					Digest:       manifest.Digest,
					PushedAt:     m.PushedAt,
				})
				if err != nil {
					return err
				}
			}

			//after making all DB changes, but before committing the DB transaction,
			//write the manifest into the backend
			return p.sd.WriteManifest(account, repo.Name, manifest.Digest, m.Contents)
		},
	)
	if err != nil {
		return nil, err
	}

	//submit audit events, but only if we are reasonably sure that we actually
	//inserted a new manifest and/or changed a tag (without this restriction, we
	//would log an audit event everytime a manifest is validated or a tag is
	//synced; before the introduction of this check, we generated millions of
	//useless audit events per month)
	if userInfo := actx.UserIdentity.UserInfo(); userInfo != nil {
		record := func(target audittools.TargetRenderer) {
			p.auditor.Record(audittools.EventParameters{
				Time:       p.timeNow(),
				Request:    actx.Request,
				User:       userInfo,
				ReasonCode: http.StatusOK,
				Action:     cadf.CreateAction,
				Target:     target,
			})
		}
		if !manifestExistsAlready {
			record(auditManifest{
				Account:    account,
				Repository: repo,
				Digest:     manifest.Digest,
			})
		}
		if m.Reference.IsTag() && !tagExistsAlready {
			record(auditTag{
				Account:    account,
				Repository: repo,
				Digest:     manifest.Digest,
				TagName:    m.Reference.Tag,
			})
		}
	}
	return manifest, nil
}

// ValidateExistingManifest validates the given manifest that already exists in the DB.
// The `now` argument will be used instead of time.Now() to accommodate unit
// tests that use a different clock.
func (p *Processor) ValidateExistingManifest(account keppel.Account, repo keppel.Repository, manifest *keppel.Manifest, now time.Time) error {
	manifestBytes, err := p.sd.ReadManifest(account, repo.Name, manifest.Digest)
	if err != nil {
		return err
	}

	//if the validation succeeds, these fields will be committed
	manifest.ValidatedAt = now
	manifest.ValidationErrorMessage = ""

	return p.validateAndStoreManifestCommon(account, repo, manifest, manifestBytes,
		func(tx *gorp.Transaction) error { return nil },
	)
}

func (p *Processor) validateAndStoreManifestCommon(account keppel.Account, repo keppel.Repository, manifest *keppel.Manifest, manifestBytes []byte, actionBeforeCommit func(*gorp.Transaction) error) error {
	//parse manifest
	manifestParsed, manifestDesc, err := keppel.ParseManifest(manifest.MediaType, manifestBytes)
	if err != nil {
		return keppel.ErrManifestInvalid.With(err.Error())
	}
	if manifest.Digest != "" && manifestDesc.Digest.String() != manifest.Digest {
		return keppel.ErrDigestInvalid.With("actual manifest digest is " + manifestDesc.Digest.String())
	}

	//fill in the fields of `manifest` that ValidateAndStoreManifest() could not
	//fill in yet ()
	manifest.Digest = manifestDesc.Digest.String()
	// ^ This field was empty until now when the user pushed a tag and therefore
	// did not supply a digest.
	manifest.MediaType = manifestDesc.MediaType
	// ^ Those two should be the same already, but if in doubt, we trust the
	// parser more than the user input.
	manifest.SizeBytes = uint64(manifestDesc.Size)
	for _, desc := range manifestParsed.BlobReferences() {
		manifest.SizeBytes += uint64(desc.Size)
	}

	return p.insideTransaction(func(tx *gorp.Transaction) error {
		refsInfo, err := findManifestReferencedObjects(tx, account, repo, manifestParsed)
		if err != nil {
			return err
		}
		manifest.SizeBytes += refsInfo.SumChildSizes

		configInfo, err := parseManifestConfig(tx, p.sd, account, manifestParsed)
		if err != nil {
			return err
		}

		//enforce account-specific validation rules on manifest, but not list manifest
		//and only when pushing (not when validating at a later point in time,
		//the set of RequiredLabels could have been changed by then)
		labelsRequired := manifest.PushedAt == manifest.ValidatedAt && account.RequiredLabels != "" &&
			manifest.MediaType != manifestlist.MediaTypeManifestList && manifest.MediaType != imagespec.MediaTypeImageIndex
		if labelsRequired {
			requiredLabels := strings.Split(account.RequiredLabels, ",")
			var missingLabels []string
			for _, l := range requiredLabels {
				if _, exists := configInfo.Labels[l]; !exists {
					missingLabels = append(missingLabels, l)
				}
			}
			if len(missingLabels) > 0 {
				msg := "missing required labels: " + strings.Join(missingLabels, ", ")
				return keppel.ErrManifestInvalid.With(msg)
			}
		}

		//for plain manifests, we report the labels from the manifest config; for
		//list manifests (which do not have a config), we instead report all the
		//labels that the constituent manifests agree on
		reportedLabels := configInfo.Labels
		if manifest.MediaType == manifestlist.MediaTypeManifestList || manifest.MediaType == imagespec.MediaTypeImageIndex {
			reportedLabels = refsInfo.CommonLabels
		}
		if len(reportedLabels) > 0 {
			labelsJSON, err := json.Marshal(reportedLabels)
			if err != nil {
				return err
			}
			manifest.LabelsJSON = string(labelsJSON)
		} else {
			manifest.LabelsJSON = ""
		}

		manifest.MinLayerCreatedAt = keppel.MinMaybeTime(refsInfo.MinCreationTime, configInfo.MinCreationTime)
		manifest.MaxLayerCreatedAt = keppel.MaxMaybeTime(refsInfo.MaxCreationTime, configInfo.MaxCreationTime)

		//create or update database entries
		err = upsertManifest(tx, *manifest, manifestBytes, p.timeNow())
		if err != nil {
			return err
		}
		err = maintainManifestBlobRefs(tx, *manifest, refsInfo.BlobRefs)
		if err != nil {
			return err
		}
		err = maintainManifestManifestRefs(tx, *manifest, refsInfo.ManifestDigests)
		if err != nil {
			return err
		}

		return actionBeforeCommit(tx)
	})
}

type blobRef struct {
	ID        int64
	MediaType string
}

// Accumulated information about all the manifests and blobs referenced by a specific manifest.
type manifestRefsInfo struct {
	BlobRefs        []blobRef
	ManifestDigests []string
	CommonLabels    map[string]string
	MinCreationTime *time.Time
	MaxCreationTime *time.Time
	SumChildSizes   uint64
}

func findManifestReferencedObjects(tx *gorp.Transaction, account keppel.Account, repo keppel.Repository, manifest keppel.ParsedManifest) (result manifestRefsInfo, err error) {
	//ensure that we don't insert duplicate entries into `blobRefs` and `manifestDigests`
	wasHandled := make(map[string]bool)

	//for all blobs referenced by this manifest...
	for _, desc := range manifest.BlobReferences() {
		if wasHandled[desc.Digest.String()] {
			continue
		}
		wasHandled[desc.Digest.String()] = true

		//check that the blob exists
		blob, err := keppel.FindBlobByRepository(tx, desc.Digest, repo)
		if err == sql.ErrNoRows {
			return manifestRefsInfo{}, keppel.ErrManifestBlobUnknown.With("").WithDetail(desc.Digest.String())
		}
		if err != nil {
			return manifestRefsInfo{}, err
		}

		//check that the blob size matches what the manifest says
		if blob.SizeBytes != uint64(desc.Size) {
			msg := fmt.Sprintf(
				"manifest references blob %s with %d bytes, but blob actually contains %d bytes",
				desc.Digest.String(), desc.Size, blob.SizeBytes)
			return manifestRefsInfo{}, keppel.ErrManifestInvalid.With(msg)
		}
		result.BlobRefs = append(result.BlobRefs, blobRef{blob.ID, desc.MediaType})
	}

	//for all manifests referenced by this manifest...
	for idx, desc := range manifest.ManifestReferences(account.PlatformFilter) {
		if wasHandled[desc.Digest.String()] {
			continue
		}
		wasHandled[desc.Digest.String()] = true

		//check that the child manifest exists
		manifest, err := keppel.FindManifest(tx, repo, desc.Digest.String())
		if err == sql.ErrNoRows {
			return manifestRefsInfo{}, keppel.ErrManifestUnknown.With("").WithDetail(desc.Digest.String())
		}
		if err != nil {
			return manifestRefsInfo{}, err
		}

		//compute the set of label values that all child manifests agree on
		var labels map[string]string
		if manifest.LabelsJSON != "" {
			err := json.Unmarshal([]byte(manifest.LabelsJSON), &labels)
			if err != nil {
				return manifestRefsInfo{}, err
			}
		}
		if idx == 0 {
			//start with the labels of the first child manifest
			result.CommonLabels = labels
		} else {
			//for each other child manifest, drop the labels where values do not match
			for key, thisValue := range labels {
				commonValue, exists := result.CommonLabels[key]
				if exists && commonValue != thisValue {
					delete(result.CommonLabels, key)
				}
			}
		}

		//compute aggregate information for all child manifests
		result.ManifestDigests = append(result.ManifestDigests, desc.Digest.String())
		result.MinCreationTime = keppel.MinMaybeTime(result.MinCreationTime, manifest.MinLayerCreatedAt)
		result.MaxCreationTime = keppel.MaxMaybeTime(result.MaxCreationTime, manifest.MaxLayerCreatedAt)
		result.SumChildSizes += manifest.SizeBytes
	}

	return result, nil
}

// Information about a manifest's config blob.
type manifestConfigInfo struct {
	Labels          map[string]string
	MinCreationTime *time.Time //across all layers
	MaxCreationTime *time.Time //across all layers
}

// Returns the list of missing labels, or nil if everything is ok.
func parseManifestConfig(tx *gorp.Transaction, sd keppel.StorageDriver, account keppel.Account, manifest keppel.ParsedManifest) (result manifestConfigInfo, err error) {
	//is this manifest an image that has labels?
	configBlob := manifest.FindImageConfigBlob()
	if configBlob == nil {
		return manifestConfigInfo{}, nil
	}

	//load the config blob
	storageID, err := tx.SelectStr(
		`SELECT storage_id FROM blobs WHERE account_name = $1 AND digest = $2`,
		account.Name, configBlob.Digest.String(),
	)
	if err != nil {
		return manifestConfigInfo{}, err
	}
	if storageID == "" {
		return manifestConfigInfo{}, keppel.ErrManifestBlobUnknown.With("").WithDetail(configBlob.Digest.String())
	}
	blobReader, _, err := sd.ReadBlob(account, storageID)
	if err != nil {
		return manifestConfigInfo{}, err
	}
	blobContents, err := io.ReadAll(blobReader)
	if err != nil {
		return manifestConfigInfo{}, err
	}
	err = blobReader.Close()
	if err != nil {
		return manifestConfigInfo{}, err
	}

	//the Docker v2 and OCI formats are very similar; they're both JSON and have
	//the labels in the same place, so we can use a single code path for both
	var data struct {
		Config struct {
			Labels map[string]string `json:"labels"`
		} `json:"config"`
		History []struct {
			Created *time.Time `json:"created"`
		} `json:"history"`
	}
	err = json.Unmarshal(blobContents, &data)
	if err != nil {
		return manifestConfigInfo{}, err
	}
	result.Labels = data.Config.Labels

	// collect layer creation times (but ignore layers with a creation timestamp
	// equal to the Unix epoch, like for distroless [1], since such timestamps
	// are caused by a reproducible build and not indicative of the actual build
	// time)
	//
	// [1] Ref: <https://github.com/GoogleContainerTools/distroless/issues/112>
	for _, v := range data.History {
		if v.Created != nil && v.Created.Unix() != 0 {
			result.MinCreationTime = keppel.MinMaybeTime(result.MinCreationTime, v.Created)
			result.MaxCreationTime = keppel.MaxMaybeTime(result.MaxCreationTime, v.Created)
		}
	}

	return result, nil
}

var upsertManifestQuery = sqlext.SimplifyWhitespace(`
	INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at, labels_json, min_layer_created_at, max_layer_created_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	ON CONFLICT (repo_id, digest) DO UPDATE
		SET size_bytes = EXCLUDED.size_bytes, validated_at = EXCLUDED.validated_at, labels_json = EXCLUDED.labels_json,
		min_layer_created_at = EXCLUDED.min_layer_created_at, max_layer_created_at = EXCLUDED.max_layer_created_at
`)

var upsertManifestContentQuery = sqlext.SimplifyWhitespace(`
	INSERT INTO manifest_contents (repo_id, digest, content)
	VALUES ($1, $2, $3)
	ON CONFLICT (repo_id, digest) DO UPDATE
		SET content = EXCLUDED.content
`)

var upsertManifestVulnerabilityInfo = sqlext.SimplifyWhitespace(`
	INSERT INTO vuln_info (repo_id, digest, status, message, next_check_at)
	VALUES ($1, $2, $3, $4, $5)
	ON CONFLICT DO NOTHING
`)

func upsertManifest(db gorp.SqlExecutor, m keppel.Manifest, manifestBytes []byte, timeNow time.Time) error {
	_, err := db.Exec(upsertManifestQuery, m.RepositoryID, m.Digest, m.MediaType, m.SizeBytes, m.PushedAt, m.ValidatedAt, m.LabelsJSON, m.MinLayerCreatedAt, m.MaxLayerCreatedAt)
	if err != nil {
		return err
	}
	_, err = db.Exec(upsertManifestContentQuery, m.RepositoryID, m.Digest, manifestBytes)
	if err != nil {
		return err
	}

	_, err = db.Exec(upsertManifestVulnerabilityInfo, m.RepositoryID, m.Digest, clair.PendingVulnerabilityStatus, "", timeNow)
	return err
}

var upsertTagQuery = sqlext.SimplifyWhitespace(`
	INSERT INTO tags (repo_id, name, digest, pushed_at)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (repo_id, name) DO UPDATE
		SET digest = EXCLUDED.digest,
			-- only set "pushed_at" when the tag is actually moving to a different manifest
			pushed_at = (CASE WHEN tags.digest = EXCLUDED.digest THEN tags.pushed_at ELSE EXCLUDED.pushed_at END),
			-- merge "last_pulled_at" when staying on the same manifest, otherwise use only new value
			last_pulled_at = (CASE WHEN tags.digest = EXCLUDED.digest THEN GREATEST(tags.last_pulled_at, EXCLUDED.last_pulled_at) ELSE EXCLUDED.last_pulled_at END)
`)

func upsertTag(db gorp.SqlExecutor, t keppel.Tag) error {
	_, err := db.Exec(upsertTagQuery, t.RepositoryID, t.Name, t.Digest, t.PushedAt)
	return err
}

func maintainManifestBlobRefs(tx *gorp.Transaction, m keppel.Manifest, referencedBlobs []blobRef) error {
	//maintain media type on blobs (we have no way of knowing the media type of a
	//blob when it gets uploaded by itself, but manifests always include the
	//media type of each blob referenced therein; therefore now is our only
	//chance to persist this information for future use)
	query := `UPDATE blobs SET media_type = $1 WHERE id = $2 AND media_type != $1`
	err := sqlext.WithPreparedStatement(tx, query, func(stmt *sql.Stmt) error {
		for _, blobRef := range referencedBlobs {
			_, err := stmt.Exec(blobRef.MediaType, blobRef.ID)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	//find existing manifest_blob_refs entries for this manifest
	isExistingBlobIDRef := make(map[int64]bool)
	query = `SELECT blob_id FROM manifest_blob_refs WHERE repo_id = $1 AND digest = $2`
	err = sqlext.ForeachRow(tx, query, []interface{}{m.RepositoryID, m.Digest}, func(rows *sql.Rows) error {
		var blobID int64
		err := rows.Scan(&blobID)
		isExistingBlobIDRef[blobID] = true
		return err
	})
	if err != nil {
		return err
	}

	//create missing manifest_blob_refs
	if len(referencedBlobs) > 0 {
		err = sqlext.WithPreparedStatement(tx,
			`INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES ($1, $2, $3)`,
			func(stmt *sql.Stmt) error {
				for _, blobRef := range referencedBlobs {
					if isExistingBlobIDRef[blobRef.ID] {
						delete(isExistingBlobIDRef, blobRef.ID) //see below for why we do this
						continue
					}

					_, err := stmt.Exec(m.RepositoryID, m.Digest, blobRef.ID)
					if err != nil {
						return err
					}
				}
				return nil
			},
		)
		if err != nil {
			return err
		}
	}

	//delete superfluous manifest_blob_refs (because we deleted from
	//`isExistingBlobIDRef` in the previous loop, all entries left in it are
	//definitely not in `referencedBlobIDs` and therefore need to be deleted)
	if len(isExistingBlobIDRef) > 0 {
		err = sqlext.WithPreparedStatement(tx,
			`DELETE FROM manifest_blob_refs WHERE repo_id = $1 AND digest = $2 AND blob_id = $3`,
			func(stmt *sql.Stmt) error {
				for blobID := range isExistingBlobIDRef {
					_, err := stmt.Exec(m.RepositoryID, m.Digest, blobID)
					if err != nil {
						return err
					}
				}
				return nil
			},
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func maintainManifestManifestRefs(tx *gorp.Transaction, m keppel.Manifest, referencedManifestDigests []string) error {
	//find existing manifest_manifest_refs entries for this manifest
	isExistingManifestDigestRef := make(map[string]bool)
	query := `SELECT child_digest FROM manifest_manifest_refs WHERE repo_id = $1 AND parent_digest = $2`
	err := sqlext.ForeachRow(tx, query, []interface{}{m.RepositoryID, m.Digest}, func(rows *sql.Rows) error {
		var childDigest string
		err := rows.Scan(&childDigest)
		isExistingManifestDigestRef[childDigest] = true
		return err
	})
	if err != nil {
		return err
	}

	//create missing manifest_manifest_refs
	if len(referencedManifestDigests) > 0 {
		err = sqlext.WithPreparedStatement(tx,
			`INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES ($1, $2, $3)`,
			func(stmt *sql.Stmt) error {
				for _, childDigest := range referencedManifestDigests {
					if isExistingManifestDigestRef[childDigest] {
						delete(isExistingManifestDigestRef, childDigest) //see below for why we do this
						continue
					}

					_, err := stmt.Exec(m.RepositoryID, m.Digest, childDigest)
					if err != nil {
						return err
					}
				}
				return nil
			},
		)
		if err != nil {
			return err
		}
	}

	//delete superfluous manifest_manifest_refs (because we deleted from
	//`isExistingManifestDigestRef` in the previous loop, all entries left in it
	//are definitely not in `referencedManifestDigests` and therefore need to be
	//deleted)
	if len(isExistingManifestDigestRef) > 0 {
		err = sqlext.WithPreparedStatement(tx,
			`DELETE FROM manifest_manifest_refs WHERE repo_id = $1 AND parent_digest = $2 AND child_digest = $3`,
			func(stmt *sql.Stmt) error {
				for childDigest := range isExistingManifestDigestRef {
					_, err := stmt.Exec(m.RepositoryID, m.Digest, childDigest)
					if err != nil {
						return err
					}
				}
				return nil
			},
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// UpstreamManifestMissingError is returned from ReplicateManifest when a
// manifest is legitimately nonexistent on upstream (i.e. returning a valid 404 error in the correct format).
type UpstreamManifestMissingError struct {
	Ref   keppel.ManifestReference
	Inner error
}

// Error implements the builtin/error interface.
func (e UpstreamManifestMissingError) Error() string {
	return e.Inner.Error()
}

// ReplicateManifest replicates the manifest from its account's upstream registry.
// On success, the manifest's metadata and contents are returned.
func (p *Processor) ReplicateManifest(account keppel.Account, repo keppel.Repository, reference keppel.ManifestReference, actx keppel.AuditContext) (*keppel.Manifest, []byte, error) {
	manifestBytes, manifestMediaType, err := p.downloadManifestViaInboundCache(account, repo, reference)
	if err != nil {
		if errorIsManifestNotFound(err) {
			return nil, nil, UpstreamManifestMissingError{reference, err}
		}
		return nil, nil, err
	}

	//parse the manifest to discover references to other manifests and blobs
	manifestParsed, _, err := keppel.ParseManifest(manifestMediaType, manifestBytes)
	if err != nil {
		return nil, nil, keppel.ErrManifestInvalid.With(err.Error())
	}

	//replicate referenced manifests recursively if required
	for _, desc := range manifestParsed.ManifestReferences(account.PlatformFilter) {
		_, err := keppel.FindManifest(p.db, repo, desc.Digest.String())
		if err == sql.ErrNoRows {
			_, _, err = p.ReplicateManifest(account, repo, keppel.ManifestReference{Digest: desc.Digest}, actx)
		}
		if err != nil {
			return nil, nil, err
		}
	}

	//mark all missing blobs as pending replication
	for _, desc := range manifestParsed.BlobReferences() {
		//mark referenced blobs as pending replication if not replicated yet
		blob, err := p.FindBlobOrInsertUnbackedBlob(desc, account)
		if err != nil {
			return nil, nil, err
		}
		//also ensure that the blob is mounted in this repo (this is also
		//important if the blob exists; it may only have been replicated in a
		//different repo)
		err = keppel.MountBlobIntoRepo(p.db, *blob, repo)
		if err != nil {
			return nil, nil, err
		}
	}

	//if the manifest is an image, we need to replicate the image configuration
	//blob immediately because ValidateAndStoreManifest() uses it for validation
	//purposes
	configBlobDesc := manifestParsed.FindImageConfigBlob()
	if configBlobDesc != nil {
		configBlob, err := keppel.FindBlobByAccountName(p.db, configBlobDesc.Digest, account)
		if err != nil {
			return nil, nil, err
		}
		if configBlob.StorageID == "" {
			_, err = p.ReplicateBlob(*configBlob, account, repo, nil)
			if err != nil {
				return nil, nil, err
			}
		}
	}

	manifest, err := p.ValidateAndStoreManifest(account, repo, IncomingManifest{
		Reference: reference,
		MediaType: manifestMediaType,
		Contents:  manifestBytes,
		PushedAt:  p.timeNow(),
	}, actx)
	return manifest, manifestBytes, err
}

// CheckManifestOnPrimary checks if the given manifest exists on its account's
// upstream registry. If not, false is returned, An error is returned only if
// the account is not a replica, or if the upstream registry cannot be queried.
func (p *Processor) CheckManifestOnPrimary(account keppel.Account, repo keppel.Repository, reference keppel.ManifestReference) (bool, error) {
	_, _, err := p.downloadManifestViaInboundCache(account, repo, reference)
	if err != nil {
		if errorIsManifestNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func errorIsManifestNotFound(err error) bool {
	if rerr, ok := err.(*keppel.RegistryV2Error); ok {
		//ErrManifestUnknown: manifest was deleted
		//ErrNameUnknown: repo was deleted
		//"NOT_FOUND": not defined by the spec, but observed in the wild with Harbor
		return rerr.Code == keppel.ErrManifestUnknown || rerr.Code == keppel.ErrNameUnknown || rerr.Code == "NOT_FOUND"
	}
	return false
}

func errorIsUpstreamRateLimit(err error) bool {
	if rerr, ok := err.(*keppel.RegistryV2Error); ok {
		return rerr.Code == keppel.ErrTooManyRequests
	}
	return false
}

// Downloads a manifest from an account's upstream using
// RepoClient.DownloadManifest(), but also takes into account the inbound cache.
func (p *Processor) downloadManifestViaInboundCache(account keppel.Account, repo keppel.Repository, ref keppel.ManifestReference) (manifestBytes []byte, manifestMediaType string, err error) {
	c, err := p.getRepoClientForUpstream(account, repo)
	if err != nil {
		return nil, "", err
	}

	//try loading the manifest from the cache
	imageRef := keppel.ImageReference{
		Host:      c.Host,
		RepoName:  c.RepoName,
		Reference: ref,
	}
	labels := prometheus.Labels{"external_hostname": c.Host}
	manifestBytes, manifestMediaType, err = p.icd.LoadManifest(imageRef, p.timeNow())
	if err == nil {
		InboundManifestCacheHitCounter.With(labels).Inc()
		return manifestBytes, manifestMediaType, nil
	}
	if err != sql.ErrNoRows {
		return nil, "", err
	}

	//cache miss -> download from actual upstream registry
	manifestBytes, manifestMediaType, err = c.DownloadManifest(ref, &client.DownloadManifestOpts{
		DoNotCountTowardsLastPulled: true,
	})
	if err != nil && account.ExternalPeerURL != "" && errorIsUpstreamRateLimit(err) {
		//when a pull from an external registry runs into a rate limit, ask a
		//random peer to retry the pull for us; they might be successful since
		//rate limits are usually per source IP
		var ok bool
		manifestBytes, manifestMediaType, ok = p.downloadManifestViaPullDelegation(imageRef, account.ExternalPeerUserName, account.ExternalPeerPassword)
		if ok {
			err = nil
		}
	}
	if err != nil {
		return nil, "", err
	}

	//successfully downloaded manifest -> fill cache
	err = p.icd.StoreManifest(imageRef, manifestBytes, manifestMediaType, p.timeNow())
	if err != nil {
		return nil, "", err
	}

	InboundManifestCacheMissCounter.With(labels).Inc()
	return manifestBytes, manifestMediaType, nil
}

// Uses the peering API to ask another peer to downloads a manifest from an
// external registry for us. This gets used when the external registry denies
// the pull to us because we hit our rate limit.
func (p *Processor) downloadManifestViaPullDelegation(imageRef keppel.ImageReference, userName, password string) (respBytes []byte, contentType string, success bool) {
	//select a peer at random
	var peer keppel.Peer
	err := p.db.SelectOne(&peer, `SELECT * FROM peers WHERE our_password != '' ORDER BY RANDOM() LIMIT 1`)
	if err == sql.ErrNoRows {
		//no peers set up - just skip this step without logging anything
		return nil, "", false
	}
	if err != nil {
		logg.Error("while trying to select a peer for pull delegation: %s", err.Error())
		return nil, "", false
	}

	//get token for peer
	peerToken, err := auth.GetPeerToken(p.cfg, peer, auth.PeerAPIScope)
	if err != nil {
		logg.Error("while trying to get a peer token for pull delegation: %s", err.Error())
		return nil, "", false
	}

	//build request
	reqURL := fmt.Sprintf("https://%s/peer/v1/delegatedpull/%s/v2/%s/manifests/%s",
		peer.HostName, imageRef.Host, imageRef.RepoName, imageRef.Reference)
	req, err := http.NewRequest(http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		logg.Error("while trying to build a pull delegation request for %s: %s", imageRef.String(), err.Error())
		return nil, "", false
	}
	req.Header.Set("Authorization", "Bearer "+peerToken)
	req.Header.Set("X-Keppel-Delegated-Pull-Username", userName)
	req.Header.Set("X-Keppel-Delegated-Pull-Password", password)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logg.Error("during GET %s: %s", reqURL, err.Error())
		return nil, "", false
	}
	defer resp.Body.Close()
	respBytes, err = io.ReadAll(resp.Body)
	if err != nil {
		logg.Error("during GET %s: %s", reqURL, err.Error())
		return nil, "", false
	}

	if resp.StatusCode != http.StatusOK {
		logg.Error("during GET %s: expected 200, got %d with response: %s",
			req.URL, resp.StatusCode, string(respBytes))
		return nil, "", false
	}
	return respBytes, resp.Header.Get("Content-Type"), true
}

// DeleteManifest deletes the given manifest from both the database and the
// backing storage.
//
// If the manifest does not exist, sql.ErrNoRows is returned.
func (p *Processor) DeleteManifest(account keppel.Account, repo keppel.Repository, digestStr string, actx keppel.AuditContext) error {
	result, err := p.db.Exec(
		//this also deletes tags referencing this manifest because of "ON DELETE CASCADE"
		`DELETE FROM manifests WHERE repo_id = $1 AND digest = $2`,
		repo.ID, digestStr)
	if err != nil {
		otherDigest, err2 := p.db.SelectStr(
			`SELECT parent_digest FROM manifest_manifest_refs WHERE repo_id = $1 AND child_digest = $2`,
			repo.ID, digestStr)
		// more than one manifest is referenced by another manifest
		if otherDigest != "" && err2 == nil {
			return fmt.Errorf("cannot delete a manifest which is referenced by the manifest %s", otherDigest)
		}
		// if the SELECT failed return the previous error to not shadow it
		return err
	}
	rowsDeleted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsDeleted == 0 {
		return sql.ErrNoRows
	}

	//We delete in the storage *after* the deletion is durable in the DB to be
	//extra sure that we did not break any constraints (esp. manifest-manifest
	//refs and manifest-blob refs) that the DB enforces. Doing things in this
	//order might mean that, if DeleteManifest fails, we're left with a manifest
	//in the backing storage that is not referenced in the DB anymore, but this
	//is not a huge problem since the janitor can clean those up after the fact.
	//What's most important is that we don't lose any data in the backing storage
	//while it is still referenced in the DB.
	//
	//Also, the DELETE statement could fail if some concurrent process created a
	//manifest reference in the meantime. If that happens, and we have already
	//deleted the manifest in the backing storage, we've caused an inconsistency
	//that we cannot recover from.
	err = p.sd.DeleteManifest(account, repo.Name, digestStr)
	if err != nil {
		return err
	}

	if userInfo := actx.UserIdentity.UserInfo(); userInfo != nil {
		p.auditor.Record(audittools.EventParameters{
			Time:       p.timeNow(),
			Request:    actx.Request,
			User:       userInfo,
			ReasonCode: http.StatusOK,
			Action:     cadf.DeleteAction,
			Target: auditManifest{
				Account:    account,
				Repository: repo,
				Digest:     digestStr,
			},
		})
	}

	return nil
}

// DeleteTag deletes the given tag from the database. The manifest is not deleted.
// If the tag does not exist, sql.ErrNoRows is returned.
func (p *Processor) DeleteTag(account keppel.Account, repo keppel.Repository, tagName string, actx keppel.AuditContext) error {
	parsedDigest, err := p.db.SelectStr(
		`DELETE FROM tags WHERE repo_id = $1 AND name = $2 RETURNING digest`,
		repo.ID, tagName)
	if err != nil {
		return err
	}
	if parsedDigest == "" {
		return sql.ErrNoRows
	}

	if userInfo := actx.UserIdentity.UserInfo(); userInfo != nil {
		p.auditor.Record(audittools.EventParameters{
			Time:       p.timeNow(),
			Request:    actx.Request,
			User:       userInfo,
			ReasonCode: http.StatusOK,
			Action:     cadf.DeleteAction,
			Target: auditTag{
				Account:    account,
				Repository: repo,
				Digest:     parsedDigest,
				TagName:    tagName,
			},
		})
	}

	return nil
}

// auditManifest is an audittools.TargetRenderer.
type auditManifest struct {
	Account    keppel.Account
	Repository keppel.Repository
	Digest     string
}

// Render implements the audittools.TargetRenderer interface.
func (a auditManifest) Render() cadf.Resource {
	return cadf.Resource{
		TypeURI:   "docker-registry/account/repository/manifest",
		Name:      fmt.Sprintf("%s@%s", a.Repository.FullName(), a.Digest),
		ID:        a.Digest,
		ProjectID: a.Account.AuthTenantID,
	}
}

// auditTag is an audittools.TargetRenderer.
type auditTag struct {
	Account    keppel.Account
	Repository keppel.Repository
	Digest     string
	TagName    string
}

// Render implements the audittools.TargetRenderer interface.
func (a auditTag) Render() cadf.Resource {
	return cadf.Resource{
		TypeURI:   "docker-registry/account/repository/tag",
		Name:      fmt.Sprintf("%s:%s", a.Repository.FullName(), a.TagName),
		ID:        a.Digest,
		ProjectID: a.Account.AuthTenantID,
	}
}
