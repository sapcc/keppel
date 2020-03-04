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
	"io/ioutil"
	"strings"
	"time"

	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/manifestlist"
	"github.com/docker/distribution/manifest/ocischema"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/sapcc/keppel/internal/keppel"
	"gopkg.in/gorp.v2"
)

//IncomingManifest contains information about a manifest uploaded by the user
//(or downloaded from a peer registry in the case of replication).
type IncomingManifest struct {
	RepoName  string
	Reference keppel.ManifestReference
	MediaType string
	Contents  []byte
	PushedAt  time.Time //usually time.Now(), but can be different in unit tests
}

//ValidateAndStoreManifest validates the given manifest and stores it under the
//given reference. If the reference is a digest, it is validated. Otherwise, a
//tag with that name is created that points to the new manifest.
func (p *Processor) ValidateAndStoreManifest(account keppel.Account, m IncomingManifest) (*keppel.Manifest, error) {
	err := p.checkQuotaForManifestPush(account)
	if err != nil {
		return nil, err
	}

	//validate manifest
	manifest, manifestDesc, err := distribution.UnmarshalManifest(m.MediaType, m.Contents)
	if err != nil {
		return nil, keppel.ErrManifestInvalid.With(err.Error())
	}
	if m.Reference.IsDigest() && manifestDesc.Digest != m.Reference.Digest {
		return nil, keppel.ErrDigestInvalid.With("actual manifest digest is " + manifestDesc.Digest.String())
	}

	var dbManifest *keppel.Manifest
	err = p.insideTransaction(func(tx *gorp.Transaction) error {
		repo, err := keppel.FindOrCreateRepository(tx, m.RepoName, account)
		if err != nil {
			return err
		}

		//when a manifest is pushed into an account with replication enabled, it's
		//because we're replicating a manifest from upstream; in this case, the
		//referenced blobs and manifests will be replicated later and we skip the
		//corresponding validation steps
		hasReferencedObjects := account.UpstreamPeerHostName == ""
		var (
			referencedBlobIDs         []int64
			referencedManifestDigests []string
		)

		if hasReferencedObjects {
			referencedBlobIDs, referencedManifestDigests, err = findManifestReferencedObjects(tx, account, *repo, manifest)
			if err != nil {
				return err
			}

			//enforce account-specific validation rules on manifest
			if account.RequiredLabels != "" {
				requiredLabels := strings.Split(account.RequiredLabels, ",")
				missingLabels, err := checkManifestHasRequiredLabels(tx, p.sd, account, manifest, requiredLabels)
				if err != nil {
					return err
				}
				if len(missingLabels) > 0 {
					msg := "missing required labels: " + strings.Join(missingLabels, ", ")
					return keppel.ErrManifestInvalid.With(msg)
				}
			}
		}

		//compute total size of image
		sizeBytes := uint64(manifestDesc.Size)
		for _, desc := range manifest.References() {
			sizeBytes += uint64(desc.Size)
		}

		//create new database entries
		dbManifest = &keppel.Manifest{
			RepositoryID: repo.ID,
			Digest:       manifestDesc.Digest.String(),
			MediaType:    manifestDesc.MediaType,
			SizeBytes:    sizeBytes,
			PushedAt:     m.PushedAt,
			ValidatedAt:  m.PushedAt,
		}
		err = upsertManifest(tx, *dbManifest)
		if err != nil {
			return err
		}
		if m.Reference.IsTag() {
			err = upsertTag(tx, keppel.Tag{
				RepositoryID: repo.ID,
				Name:         m.Reference.Tag,
				Digest:       manifestDesc.Digest.String(),
				PushedAt:     m.PushedAt,
			})
			if err != nil {
				return err
			}
		}

		//persist manifest-blob references into the DB
		_, err = tx.Exec(`DELETE FROM manifest_blob_refs WHERE repo_id = $1 AND digest = $2`,
			repo.ID, manifestDesc.Digest.String())
		if err != nil {
			return err
		}
		err = keppel.WithPreparedStatement(tx,
			`INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES ($1, $2, $3)`,
			func(stmt *sql.Stmt) error {
				for _, blobID := range referencedBlobIDs {
					_, err := stmt.Exec(repo.ID, manifestDesc.Digest.String(), blobID)
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

		//persist manifest-manifest references into the DB
		_, err = tx.Exec(`DELETE FROM manifest_manifest_refs WHERE repo_id = $1 AND parent_digest = $2`,
			repo.ID, manifestDesc.Digest.String())
		if err != nil {
			return err
		}
		err = keppel.WithPreparedStatement(tx,
			`INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES ($1, $2, $3)`,
			func(stmt *sql.Stmt) error {
				for _, digest := range referencedManifestDigests {
					_, err := stmt.Exec(repo.ID, manifestDesc.Digest.String(), digest)
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

		//PUT the manifest in the backend
		return p.sd.WriteManifest(account, repo.Name, manifestDesc.Digest.String(), m.Contents)
	})
	return dbManifest, err
}

func findManifestReferencedObjects(tx *gorp.Transaction, account keppel.Account, repo keppel.Repository, manifest distribution.Manifest) (blobIDs []int64, manifestDigests []string, returnErr error) {
	for _, desc := range manifest.References() {
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
			blob, err := keppel.FindBlobByRepositoryID(tx, desc.Digest, repo.ID, account)
			if err == sql.ErrNoRows {
				return nil, nil, keppel.ErrManifestBlobUnknown.With("").WithDetail(desc.Digest.String())
			}
			if err != nil {
				return nil, nil, err
			}
			blobIDs = append(blobIDs, blob.ID)
		}
	}

	return blobIDs, manifestDigests, nil
}

//Returns the list of missing labels, or nil if everything is ok.
func checkManifestHasRequiredLabels(tx *gorp.Transaction, sd keppel.StorageDriver, account keppel.Account, manifest distribution.Manifest, requiredLabels []string) ([]string, error) {
	var configBlob distribution.Descriptor
	switch m := manifest.(type) {
	case *schema2.DeserializedManifest:
		configBlob = m.Config
	case *ocischema.DeserializedManifest:
		configBlob = m.Config
	case *manifestlist.DeserializedManifestList:
		//manifest lists only reference other manifests, they don't have labels themselves
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
			SET validated_at = EXCLUDED.validated_at
	`, m.RepositoryID, m.Digest, m.MediaType, m.SizeBytes, m.PushedAt, m.ValidatedAt)
	return err
}

func upsertTag(db gorp.SqlExecutor, t keppel.Tag) error {
	_, err := db.Exec(`
		INSERT INTO tags (repo_id, name, digest, pushed_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (repo_id, name) DO UPDATE
			SET digest = EXCLUDED.digest, pushed_at = EXCLUDED.pushed_at
	`, t.RepositoryID, t.Name, t.Digest, t.PushedAt)
	return err
}
