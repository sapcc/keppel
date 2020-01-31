/******************************************************************************
*
*  Copyright 2019 SAP SE
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

package registryv2

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/docker/distribution"
	"github.com/gorilla/mux"
	"github.com/opencontainers/go-digest"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/replication"

	//distribution.UnmarshalManifest() relies on the following packages
	//registering their manifest schemas.
	_ "github.com/docker/distribution/manifest/manifestlist"
	_ "github.com/docker/distribution/manifest/ocischema"
	_ "github.com/docker/distribution/manifest/schema2"
	//NOTE: We don't enable github.com/docker/distribution/manifest/schema1
	//anymore since it's legacy anyway and the implementation is a lot simpler
	//when we don't have to rewrite manifests between schema1 and schema2.
)

//This implements the HEAD/GET /v2/<repo>/manifests/<reference> endpoint.
func (a *API) handleGetOrHeadManifest(w http.ResponseWriter, r *http.Request) {
	account, repoName, _ := a.checkAccountAccess(w, r)
	if account == nil {
		return
	}

	reference := keppel.ParseManifestReference(mux.Vars(r)["reference"])
	dbManifest, err := a.findManifestInDB(*account, repoName, reference)
	var manifestBytes []byte

	if err != sql.ErrNoRows {
		if respondWithError(w, err) {
			return
		}
	}

	if err == sql.ErrNoRows {
		//if the manifest does not exist there, we may have the option of replicating
		//from upstream
		if account.UpstreamPeerHostName != "" {
			repo, err := a.db.FindOrCreateRepository(repoName, *account)
			if respondWithError(w, err) {
				return
			}

			repl := replication.NewReplicator(a.cfg, a.db, a.sd)
			m := replication.Manifest{
				Account:   *account,
				Repo:      *repo,
				Reference: reference,
			}
			dbManifest, manifestBytes, err = repl.ReplicateManifest(m) //pm is a keppel.PendingManifest
			if respondWithError(w, err) {
				return
			}
		} else {
			keppel.ErrManifestUnknown.With("").WithDetail(reference.Tag).WriteAsRegistryV2ResponseTo(w)
			return
		}
	} else {
		//if manifest was found in our DB, fetch the contents from the storage
		manifestBytes, err = a.sd.ReadManifest(*account, repoName, dbManifest.Digest)
	}

	//verify Accept header, if any
	acceptHeader := r.Header.Get("Accept")
	if acceptHeader != "" {
		accepted := false
		for _, acceptField := range strings.Split(acceptHeader, ",") {
			acceptField = strings.SplitN(acceptField, ";", 2)[0]
			acceptField = strings.TrimSpace(acceptField)
			if acceptField == dbManifest.MediaType {
				accepted = true
			}
		}
		if !accepted {
			msg := fmt.Sprintf("manifest type %s is not covered by Accept header", dbManifest.MediaType)
			keppel.ErrManifestUnknown.With(msg).WriteAsRegistryV2ResponseTo(w)
			return
		}
	}

	//write response
	w.Header().Set("Content-Length", strconv.FormatUint(uint64(len(manifestBytes)), 10))
	w.Header().Set("Content-Type", dbManifest.MediaType)
	w.Header().Set("Docker-Content-Digest", dbManifest.Digest)
	w.WriteHeader(http.StatusOK)
	if r.Method != "HEAD" {
		w.Write(manifestBytes)
	}
	return
}

func (a *API) findManifestInDB(account keppel.Account, repoName string, reference keppel.ManifestReference) (*keppel.Manifest, error) {
	repo, err := a.db.FindRepository(repoName, account)
	if err != nil {
		return nil, err
	}

	//resolve tag into digest if necessary
	refDigest := reference.Digest
	if reference.IsTag() {
		digestStr, err := a.db.SelectStr(
			`SELECT digest FROM tags WHERE repo_id = $1 AND name = $2`,
			repo.ID, reference.Tag,
		)
		if err != nil {
			return nil, err
		}
		if digestStr == "" {
			return nil, sql.ErrNoRows
		}
		refDigest, err = digest.Parse(digestStr)
		if err != nil {
			return nil, err
		}
	}

	var dbManifest keppel.Manifest
	err = a.db.SelectOne(&dbManifest,
		`SELECT * FROM manifests WHERE repo_id = $1 AND digest = $2`,
		repo.ID, refDigest.String(),
	)
	return &dbManifest, err
}

//This implements the DELETE /v2/<repo>/manifests/<reference> endpoint.
func (a *API) handleDeleteManifest(w http.ResponseWriter, r *http.Request) {
	account, repoName, _ := a.checkAccountAccess(w, r)
	if account == nil {
		return
	}
	repo, err := a.db.FindOrCreateRepository(repoName, *account)
	if respondWithError(w, err) {
		return
	}

	//<reference> must be a digest - the API does not allow deleting tags
	//directly (tags are deleted by deleting their current manifest using its
	//canonical digest)
	digest, err := digest.Parse(mux.Vars(r)["reference"])
	if err != nil {
		keppel.ErrDigestInvalid.With(err.Error()).WriteAsRegistryV2ResponseTo(w)
		return
	}

	//prepare deletion of database entries on our side, so that we only have to
	//commit the transaction once the backend DELETE is successful
	tx, err := a.db.Begin()
	if respondWithError(w, err) {
		return
	}
	defer keppel.RollbackUnlessCommitted(tx)
	result, err := tx.Exec(
		//this also deletes tags referencing this manifest because of "ON DELETE CASCADE"
		`DELETE FROM manifests WHERE repo_id = $1 AND digest = $2`,
		repo.ID, digest.String())
	if respondWithError(w, err) {
		return
	}
	rowsDeleted, err := result.RowsAffected()
	if respondWithError(w, err) {
		return
	}
	if rowsDeleted == 0 {
		keppel.ErrManifestUnknown.With("no such manifest").WriteAsRegistryV2ResponseTo(w)
		return
	}

	//DELETE the manifest in the backend
	err = a.sd.DeleteManifest(*account, repo.Name, digest.String())
	if respondWithError(w, err) {
		return
	}
	err = tx.Commit()
	if respondWithError(w, err) {
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

//This implements the PUT /v2/<repo>/manifests/<reference> endpoint.
func (a *API) handlePutManifest(w http.ResponseWriter, r *http.Request) {
	account, repoName, token := a.checkAccountAccess(w, r)
	if account == nil {
		return
	}

	//forbid pushing into replica accounts (BUT allow it for the internal
	//replication user, who uses this handler to perform the final PUT when a
	//manifest is replicated)
	if account.UpstreamPeerHostName != "" && token.UserName != "replication@"+a.cfg.APIPublicHostname() {
		msg := fmt.Sprintf("cannot push into replica account (push to %s/%s/%s instead!)",
			account.UpstreamPeerHostName, account.Name, repoName,
		)
		keppel.ErrDenied.With(msg).WriteAsRegistryV2ResponseTo(w)
		return
	}

	//check if user has enough quota to push a manifest
	quotas, err := a.db.FindQuotas(account.AuthTenantID)
	if respondWithError(w, err) {
		return
	}
	if quotas == nil {
		quotas = keppel.DefaultQuotas(account.AuthTenantID)
	}
	manifestUsage, err := quotas.GetManifestUsage(a.db)
	if respondWithError(w, err) {
		return
	}
	if manifestUsage >= quotas.ManifestCount {
		msg := fmt.Sprintf("manifest quota exceeded (quota = %d, usage = %d)",
			quotas.ManifestCount, manifestUsage,
		)
		keppel.ErrDenied.With(msg).WithStatus(http.StatusConflict).WriteAsRegistryV2ResponseTo(w)
		return
	}

	repo, err := a.db.FindOrCreateRepository(repoName, *account)
	if respondWithError(w, err) {
		return
	}

	reference := keppel.ParseManifestReference(mux.Vars(r)["reference"])

	//validate manifest on our side
	manifestBytes, err := ioutil.ReadAll(r.Body)
	if respondWithError(w, err) {
		return
	}
	manifest, manifestDesc, err := distribution.UnmarshalManifest(r.Header.Get("Content-Type"), manifestBytes)
	if err != nil {
		keppel.ErrManifestInvalid.With(err.Error()).WriteAsRegistryV2ResponseTo(w)
		return
	}

	//if <reference> is not a tag, it must be the digest of the manifest
	if reference.IsDigest() && manifestDesc.Digest != reference.Digest {
		keppel.ErrDigestInvalid.With("actual manifest digest is " + manifestDesc.Digest.String()).WriteAsRegistryV2ResponseTo(w)
		return
	}

	//check that all referenced blobs exist (TODO: some manifest types reference
	//other manifests, so we should look for manifests in these cases)
	for _, desc := range manifest.References() {
		_, err := a.db.FindBlobByRepositoryID(desc.Digest, repo.ID, *account)
		if err == sql.ErrNoRows {
			keppel.ErrManifestBlobUnknown.With("").WithDetail(desc.Digest.String()).WriteAsRegistryV2ResponseTo(w)
			return
		}
		if respondWithError(w, err) {
			return
		}
	}

	//compute total size of image (TODO: code duplication with ReplicateManifest())
	sizeBytes := uint64(manifestDesc.Size)
	for _, desc := range manifest.References() {
		sizeBytes += uint64(desc.Size)
	}

	//prepare new database entries, so that we only have to commit the transaction once the backend PUT is successful
	tx, err := a.db.Begin()
	if respondWithError(w, err) {
		return
	}
	defer keppel.RollbackUnlessCommitted(tx)
	err = keppel.Manifest{
		RepositoryID: repo.ID,
		Digest:       manifestDesc.Digest.String(),
		MediaType:    manifestDesc.MediaType,
		SizeBytes:    sizeBytes,
		PushedAt:     a.timeNow(),
	}.InsertIfMissing(tx)
	if respondWithError(w, err) {
		return
	}
	if reference.IsTag() {
		err = keppel.Tag{
			RepositoryID: repo.ID,
			Name:         reference.Tag,
			Digest:       manifestDesc.Digest.String(),
			PushedAt:     a.timeNow(),
		}.InsertIfMissing(tx)
		if respondWithError(w, err) {
			return
		}
	}

	//PUT the manifest in the backend
	err = a.sd.WriteManifest(*account, repo.Name, manifestDesc.Digest.String(), manifestBytes)
	if respondWithError(w, err) {
		return
	}
	err = tx.Commit()
	if respondWithError(w, err) {
		return
	}

	w.Header().Set("Content-Length", "0")
	w.Header().Set("Docker-Content-Digest", manifestDesc.Digest.String())
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/manifests/%s", repo.FullName(), manifestDesc.Digest.String()))
	w.WriteHeader(http.StatusCreated)
}
