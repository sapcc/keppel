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
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/hermes/pkg/cadf"
	"github.com/sapcc/keppel/internal/client"
	"github.com/sapcc/keppel/internal/keppel"
	"gopkg.in/gorp.v2"
)

//IncomingManifest contains information about a manifest uploaded by the user
//(or downloaded from a peer registry in the case of replication).
type IncomingManifest struct {
	Reference keppel.ManifestReference
	MediaType string
	Contents  []byte
	PushedAt  time.Time //usually time.Now(), but can be different in unit tests
}

//ValidateAndStoreManifest validates the given manifest and stores it under the
//given reference. If the reference is a digest, it is validated. Otherwise, a
//tag with that name is created that points to the new manifest.
func (p *Processor) ValidateAndStoreManifest(account keppel.Account, repo keppel.Repository, m IncomingManifest, actx keppel.AuditContext) (*keppel.Manifest, error) {
	err := p.checkQuotaForManifestPush(account)
	if err != nil {
		return nil, err
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

	if userInfo := actx.Authorization.UserInfo(); userInfo != nil {
		record := func(target audittools.TargetRenderer) {
			p.auditor.Record(audittools.EventParameters{
				Time:       p.timeNow(),
				Request:    actx.Request,
				User:       userInfo,
				ReasonCode: http.StatusOK,
				Action:     "create",
				Target:     target,
			})
		}
		record(auditManifest{
			Account:    account,
			Repository: repo,
			Digest:     manifest.Digest,
		})
		if m.Reference.IsTag() {
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

//ValidateExistingManifest validates the given manifest that already exists in the DB.
//The `now` argument will be used instead of time.Now() to accommodate unit
//tests that use a different clock.
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

type blobRef struct {
	ID        int64
	MediaType string
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
	for _, desc := range manifestParsed.ManifestReferences(account.PlatformFilter) {
		manifest.SizeBytes += uint64(desc.Size)
	}

	return p.insideTransaction(func(tx *gorp.Transaction) error {
		referencedBlobs, referencedManifestDigests, err := findManifestReferencedObjects(tx, account, repo, manifestParsed)
		if err != nil {
			return err
		}
		//enforce account-specific validation rules on manifest, but only when
		//pushing (not when validating at a later point in time, the set of
		//RequiredLabels could have been changed by then)
		labelsRequired := manifest.PushedAt == manifest.ValidatedAt && account.RequiredLabels != ""
		labels, err := getManifestLabels(tx, p.sd, account, manifestParsed)
		if err != nil {
			return err
		}
		if len(labels) > 0 {
			labelsJSON, err := json.Marshal(labels)
			if err != nil {
				return err
			}
			manifest.LabelsJSON = string(labelsJSON)

			if labelsRequired {
				requiredLabels := strings.Split(account.RequiredLabels, ",")
				var missingLabels []string
				for _, l := range requiredLabels {
					if _, exists := labels[l]; !exists {
						missingLabels = append(missingLabels, l)
					}
				}
				if len(missingLabels) > 0 {
					msg := "missing required labels: " + strings.Join(missingLabels, ", ")
					return keppel.ErrManifestInvalid.With(msg)
				}
			}
		} else {
			manifest.LabelsJSON = ""
			if labelsRequired {
				return keppel.ErrManifestInvalid.With("missing required labels: %s", account.RequiredLabels)
			}
		}

		//create or update database entries
		err = upsertManifest(tx, *manifest)
		if err != nil {
			return err
		}
		err = maintainManifestBlobRefs(tx, *manifest, referencedBlobs)
		if err != nil {
			return err
		}
		err = maintainManifestManifestRefs(tx, *manifest, referencedManifestDigests)
		if err != nil {
			return err
		}

		return actionBeforeCommit(tx)
	})
}

func findManifestReferencedObjects(tx *gorp.Transaction, account keppel.Account, repo keppel.Repository, manifest keppel.ParsedManifest) (blobRefs []blobRef, manifestDigests []string, returnErr error) {
	//ensure that we don't insert duplicate entries into `blobRefs` and `manifestDigests`
	wasHandled := make(map[string]bool)

	for _, desc := range manifest.BlobReferences() {
		if wasHandled[desc.Digest.String()] {
			continue
		}
		wasHandled[desc.Digest.String()] = true

		blob, err := keppel.FindBlobByRepository(tx, desc.Digest, repo, account)
		if err == sql.ErrNoRows {
			return nil, nil, keppel.ErrManifestBlobUnknown.With("").WithDetail(desc.Digest.String())
		}
		if err != nil {
			return nil, nil, err
		}
		if blob.SizeBytes != uint64(desc.Size) {
			msg := fmt.Sprintf(
				"manifest references blob %s with %d bytes, but blob actually contains %d bytes",
				desc.Digest.String(), desc.Size, blob.SizeBytes)
			return nil, nil, keppel.ErrManifestInvalid.With(msg)
		}
		blobRefs = append(blobRefs, blobRef{blob.ID, desc.MediaType})
	}

	for _, desc := range manifest.ManifestReferences(account.PlatformFilter) {
		if wasHandled[desc.Digest.String()] {
			continue
		}
		wasHandled[desc.Digest.String()] = true

		_, err := keppel.FindManifest(tx, repo, desc.Digest.String())
		if err == sql.ErrNoRows {
			return nil, nil, keppel.ErrManifestUnknown.With("").WithDetail(desc.Digest.String())
		}
		if err != nil {
			return nil, nil, err
		}
		manifestDigests = append(manifestDigests, desc.Digest.String())
	}

	return blobRefs, manifestDigests, nil
}

//Returns the list of missing labels, or nil if everything is ok.
func getManifestLabels(tx *gorp.Transaction, sd keppel.StorageDriver, account keppel.Account, manifest keppel.ParsedManifest) (map[string]string, error) {
	//is this manifest an image that has labels?
	configBlob := manifest.FindImageConfigBlob()
	if configBlob == nil {
		return nil, nil
	}

	//load the config blob
	storageID, err := tx.SelectStr(
		`SELECT storage_id FROM blobs WHERE account_name = $1 AND digest = $2`,
		account.Name, configBlob.Digest.String(),
	)
	if err != nil {
		return nil, err
	}
	if storageID == "" {
		return nil, keppel.ErrManifestBlobUnknown.With("").WithDetail(configBlob.Digest.String())
	}
	blobReader, _, err := sd.ReadBlob(account, storageID)
	if err != nil {
		return nil, err
	}
	blobContents, err := ioutil.ReadAll(blobReader)
	if err != nil {
		return nil, err
	}
	err = blobReader.Close()
	if err != nil {
		return nil, err
	}

	//the Docker v2 and OCI formats are very similar; they're both JSON and have
	//the labels in the same place, so we can use a single code path for both
	var data struct {
		Config struct {
			Labels map[string]string `json:"labels"`
		} `json:"config"`
	}
	err = json.Unmarshal(blobContents, &data)
	if err != nil {
		return nil, err
	}

	return data.Config.Labels, nil
}

var upsertManifestQuery = keppel.SimplifyWhitespaceInSQL(`
	INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at, labels_json)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	ON CONFLICT (repo_id, digest) DO UPDATE
		SET size_bytes = EXCLUDED.size_bytes, validated_at = EXCLUDED.validated_at, labels_json = EXCLUDED.labels_json
`)

func upsertManifest(db gorp.SqlExecutor, m keppel.Manifest) error {
	_, err := db.Exec(upsertManifestQuery, m.RepositoryID, m.Digest, m.MediaType, m.SizeBytes, m.PushedAt, m.ValidatedAt, m.LabelsJSON)
	return err
}

var upsertTagQuery = keppel.SimplifyWhitespaceInSQL(`
	INSERT INTO tags (repo_id, name, digest, pushed_at)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (repo_id, name) DO UPDATE
		SET digest = EXCLUDED.digest, last_pulled_at = EXCLUDED.last_pulled_at,
			-- only set "pushed_at" when the tag is actually moving to a different manifest
			pushed_at = (CASE WHEN tags.digest = EXCLUDED.digest THEN tags.pushed_at ELSE EXCLUDED.pushed_at END)
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
	err := keppel.WithPreparedStatement(tx, query, func(stmt *sql.Stmt) error {
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
	err = keppel.ForeachRow(tx, query, []interface{}{m.RepositoryID, m.Digest}, func(rows *sql.Rows) error {
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
		err = keppel.WithPreparedStatement(tx,
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
		err = keppel.WithPreparedStatement(tx,
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
	err := keppel.ForeachRow(tx, query, []interface{}{m.RepositoryID, m.Digest}, func(rows *sql.Rows) error {
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
		err = keppel.WithPreparedStatement(tx,
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
		err = keppel.WithPreparedStatement(tx,
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

//UpstreamManifestMissingError is returned from ReplicateManifest when a
//manifest is legitimately nonexistent on upstream (i.e. returning a valid 404 error in the correct format).
type UpstreamManifestMissingError struct {
	Ref   keppel.ManifestReference
	Inner error
}

//Error implements the builtin/error interface.
func (e UpstreamManifestMissingError) Error() string {
	return e.Inner.Error()
}

//ReplicateManifest replicates the manifest from its account's upstream registry.
//On success, the manifest's metadata and contents are returned.
func (p *Processor) ReplicateManifest(account keppel.Account, repo keppel.Repository, reference keppel.ManifestReference, actx keppel.AuditContext) (*keppel.Manifest, []byte, error) {
	//query upstream for the manifest
	c, err := p.getRepoClientForUpstream(account, repo)
	if err != nil {
		return nil, nil, err
	}
	//TODO DownloadManifest should take a keppel.ManifestReference
	manifestBytes, manifestMediaType, err := c.DownloadManifest(reference.String(), &client.DownloadManifestOpts{
		DoNotCountTowardsLastPulled: true,
	})
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

//CheckManifestOnPrimary checks if the given manifest exists on its account's
//upstream registry. If not, false is returned, An error is returned only if
//the account is not a replica, or if the upstream registry cannot be queried.
func (p *Processor) CheckManifestOnPrimary(account keppel.Account, repo keppel.Repository, reference keppel.ManifestReference) (bool, error) {
	c, err := p.getRepoClientForUpstream(account, repo)
	if err != nil {
		return false, err
	}
	_, _, err = c.DownloadManifest(reference.String(), &client.DownloadManifestOpts{
		DoNotCountTowardsLastPulled: true,
	})
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

//DeleteManifest deletes the given manifest from both the database and the
//backing storage.
//
//If the manifest does not exist, sql.ErrNoRows is returned.
func (p *Processor) DeleteManifest(account keppel.Account, repo keppel.Repository, digest string, actx keppel.AuditContext) error {
	result, err := p.db.Exec(
		//this also deletes tags referencing this manifest because of "ON DELETE CASCADE"
		`DELETE FROM manifests WHERE repo_id = $1 AND digest = $2`,
		repo.ID, digest)
	if err != nil {
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
	err = p.sd.DeleteManifest(account, repo.Name, digest)
	if err != nil {
		return err
	}

	if userInfo := actx.Authorization.UserInfo(); userInfo != nil {
		p.auditor.Record(audittools.EventParameters{
			Time:       p.timeNow(),
			Request:    actx.Request,
			User:       userInfo,
			ReasonCode: http.StatusOK,
			Action:     "delete",
			Target: auditManifest{
				Account:    account,
				Repository: repo,
				Digest:     digest,
			},
		})
	}

	return nil
}

//DeleteTag deletes the given tag from the database. The manifest is not deleted.
//If the tag does not exist, sql.ErrNoRows is returned.
func (p *Processor) DeleteTag(account keppel.Account, repo keppel.Repository, tagName string, actx keppel.AuditContext) error {
	digest, err := p.db.SelectStr(
		`DELETE FROM tags WHERE repo_id = $1 AND name = $2 RETURNING digest`,
		repo.ID, tagName)
	if err != nil {
		return err
	}
	if digest == "" {
		return sql.ErrNoRows
	}

	if userInfo := actx.Authorization.UserInfo(); userInfo != nil {
		p.auditor.Record(audittools.EventParameters{
			Time:       p.timeNow(),
			Request:    actx.Request,
			User:       userInfo,
			ReasonCode: http.StatusOK,
			Action:     "delete",
			Target: auditTag{
				Account:    account,
				Repository: repo,
				Digest:     digest,
				TagName:    tagName,
			},
		})
	}

	return nil
}

//auditManifest is an audittools.TargetRenderer.
type auditManifest struct {
	Account    keppel.Account
	Repository keppel.Repository
	Digest     string
}

//Render implements the audittools.TargetRenderer interface.
func (a auditManifest) Render() cadf.Resource {
	return cadf.Resource{
		TypeURI:   "docker-registry/account/repository/manifest",
		Name:      fmt.Sprintf("%s@%s", a.Repository.FullName(), a.Digest),
		ID:        a.Digest,
		ProjectID: a.Account.AuthTenantID,
	}
}

//auditTag is an audittools.TargetRenderer.
type auditTag struct {
	Account    keppel.Account
	Repository keppel.Repository
	Digest     string
	TagName    string
}

//Render implements the audittools.TargetRenderer interface.
func (a auditTag) Render() cadf.Resource {
	return cadf.Resource{
		TypeURI:   "docker-registry/account/repository/tag",
		Name:      fmt.Sprintf("%s:%s", a.Repository.FullName(), a.TagName),
		ID:        a.Digest,
		ProjectID: a.Account.AuthTenantID,
	}
}
