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
	"strings"
	"time"

	"github.com/docker/distribution"
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
func (p *Processor) ValidateAndStoreManifest(account keppel.Account, repo keppel.Repository, m IncomingManifest) (*keppel.Manifest, error) {
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
	return manifest, err
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

func (p *Processor) validateAndStoreManifestCommon(account keppel.Account, repo keppel.Repository, manifest *keppel.Manifest, manifestBytes []byte, actionBeforeCommit func(*gorp.Transaction) error) error {
	//parse manifest
	manifestParsed, manifestDesc, err := distribution.UnmarshalManifest(manifest.MediaType, manifestBytes)
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
	for _, desc := range manifestParsed.References() {
		manifest.SizeBytes += uint64(desc.Size)
	}

	return p.insideTransaction(func(tx *gorp.Transaction) error {
		referencedBlobIDs, referencedManifestDigests, err := findManifestReferencedObjects(tx, account, repo, manifestParsed)
		if err != nil {
			return err
		}

		//enforce account-specific validation rules on manifest
		if account.RequiredLabels != "" {
			requiredLabels := strings.Split(account.RequiredLabels, ",")
			missingLabels, err := checkManifestHasRequiredLabels(tx, p.sd, account, manifestParsed, requiredLabels)
			if err != nil {
				return err
			}
			if len(missingLabels) > 0 {
				msg := "missing required labels: " + strings.Join(missingLabels, ", ")
				return keppel.ErrManifestInvalid.With(msg)
			}
		}

		//create or update database entries
		err = upsertManifest(tx, *manifest)
		if err != nil {
			return err
		}
		err = maintainManifestBlobRefs(tx, *manifest, referencedBlobIDs)
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

func findManifestReferencedObjects(tx *gorp.Transaction, account keppel.Account, repo keppel.Repository, manifest distribution.Manifest) (blobIDs []int64, manifestDigests []string, returnErr error) {
	//ensure that we don't insert duplicate entries into `blobIDs` and `manifestDigests`
	wasHandled := make(map[string]bool)

	for _, desc := range manifest.References() {
		if wasHandled[desc.Digest.String()] {
			continue
		}
		wasHandled[desc.Digest.String()] = true

		if keppel.IsManifestMediaType(desc.MediaType) {
			_, err := keppel.FindManifest(tx, repo, desc.Digest.String())
			if err == sql.ErrNoRows {
				return nil, nil, keppel.ErrManifestUnknown.With("").WithDetail(desc.Digest.String())
			}
			if err != nil {
				return nil, nil, err
			}
			manifestDigests = append(manifestDigests, desc.Digest.String())
		} else {
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
			blobIDs = append(blobIDs, blob.ID)
		}
	}

	return blobIDs, manifestDigests, nil
}

//Returns the list of missing labels, or nil if everything is ok.
func checkManifestHasRequiredLabels(tx *gorp.Transaction, sd keppel.StorageDriver, account keppel.Account, manifest distribution.Manifest, requiredLabels []string) ([]string, error) {
	//is this manifest an image that has labels?
	configBlob := keppel.FindImageConfigBlob(manifest)
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
			Labels map[string]interface{} `json:"labels"`
		} `json:"config"`
	}
	err = json.Unmarshal(blobContents, &data)
	if err != nil {
		return nil, err
	}

	var missingLabels []string
	for _, label := range requiredLabels {
		if _, exists := data.Config.Labels[label]; !exists {
			missingLabels = append(missingLabels, label)
		}
	}
	return missingLabels, nil
}

func upsertManifest(db gorp.SqlExecutor, m keppel.Manifest) error {
	_, err := db.Exec(`
		INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (repo_id, digest) DO UPDATE
			SET size_bytes = EXCLUDED.size_bytes, validated_at = EXCLUDED.validated_at
	`, m.RepositoryID, m.Digest, m.MediaType, m.SizeBytes, m.PushedAt, m.ValidatedAt)
	return err
}

func upsertTag(db gorp.SqlExecutor, t keppel.Tag) error {
	_, err := db.Exec(`
		INSERT INTO tags (repo_id, name, digest, pushed_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (repo_id, name) DO UPDATE
			SET digest = EXCLUDED.digest, pushed_at = EXCLUDED.pushed_at, last_pulled_at = EXCLUDED.last_pulled_at
	`, t.RepositoryID, t.Name, t.Digest, t.PushedAt)
	return err
}

func maintainManifestBlobRefs(tx *gorp.Transaction, m keppel.Manifest, referencedBlobIDs []int64) error {
	//find existing manifest_blob_refs entries for this manifest
	isExistingBlobIDRef := make(map[int64]bool)
	query := `SELECT blob_id FROM manifest_blob_refs WHERE repo_id = $1 AND digest = $2`
	err := keppel.ForeachRow(tx, query, []interface{}{m.RepositoryID, m.Digest}, func(rows *sql.Rows) error {
		var blobID int64
		err := rows.Scan(&blobID)
		isExistingBlobIDRef[blobID] = true
		return err
	})
	if err != nil {
		return err
	}

	//create missing manifest_blob_refs
	if len(referencedBlobIDs) > 0 {
		err = keppel.WithPreparedStatement(tx,
			`INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES ($1, $2, $3)`,
			func(stmt *sql.Stmt) error {
				for _, blobID := range referencedBlobIDs {
					if isExistingBlobIDRef[blobID] {
						delete(isExistingBlobIDRef, blobID) //see below for why we do this
						continue
					}

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

//ReplicateManifest replicates the manifest from its account's upstream registry.
//On success, the manifest's metadata and contents are returned.
func (p *Processor) ReplicateManifest(account keppel.Account, repo keppel.Repository, reference keppel.ManifestReference) (*keppel.Manifest, []byte, error) {
	//query upstream for the manifest
	client, err := p.getRepoClientForUpstream(account, repo)
	if err != nil {
		return nil, nil, err
	}
	manifestBytes, manifestMediaType, err := client.DownloadManifest(reference.String()) //TODO DownloadManifest should take a keppel.ManifestReference
	if err != nil {
		return nil, nil, err
	}

	//parse the manifest to discover references to other manifests and blobs
	manifestParsed, _, err := distribution.UnmarshalManifest(manifestMediaType, manifestBytes)
	if err != nil {
		return nil, nil, keppel.ErrManifestInvalid.With(err.Error())
	}

	//mark all missing blobs as pending replication
	for _, desc := range manifestParsed.References() {
		if keppel.IsManifestMediaType(desc.MediaType) {
			//replicate referenced manifests recursively if required
			_, err := keppel.FindManifest(p.db, repo, desc.Digest.String())
			if err == sql.ErrNoRows {
				_, _, err = p.ReplicateManifest(account, repo, keppel.ManifestReference{Digest: desc.Digest})
			}
			if err != nil {
				return nil, nil, err
			}
		} else {
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
	}

	//if the manifest is an image, we need to replicate the image configuration
	//blob immediately because ValidateAndStoreManifest() uses it for validation
	//purposes
	configBlobDesc := keppel.FindImageConfigBlob(manifestParsed)
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
	})
	return manifest, manifestBytes, err
}

//CheckManifestOnPrimary checks if the given manifest exists on its account's
//upstream registry. If not, false is returned, An error is returned only if
//the account is not a replica, or if the upstream registry cannot be queried.
func (p *Processor) CheckManifestOnPrimary(account keppel.Account, repo keppel.Repository, reference keppel.ManifestReference) (bool, error) {
	client, err := p.getRepoClientForUpstream(account, repo)
	if err != nil {
		return false, err
	}
	_, _, err = client.DownloadManifest(reference.String())
	switch err := err.(type) {
	case nil:
		return true, nil
	case *keppel.RegistryV2Error:
		if err.Code == keppel.ErrManifestUnknown {
			return false, nil
		}
	}
	return false, err
}
