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
	"io"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/replication"
)

//This implements the GET/HEAD /v2/<account>/<repository>/blobs/<digest> endpoint.
func (a *API) handleGetOrHeadBlob(w http.ResponseWriter, r *http.Request) {
	account, repoName, _ := a.checkAccountAccess(w, r)
	if account == nil {
		return
	}

	blobDigest, err := digest.Parse(mux.Vars(r)["digest"])
	if err != nil {
		keppel.ErrDigestInvalid.With(err.Error()).WriteAsRegistryV2ResponseTo(w)
		return
	}

	//locate this blob from the DB
	blob, err := a.db.FindBlobByRepositoryName(blobDigest, repoName, *account)
	if err == sql.ErrNoRows {
		//if the blob does not exist here, we may have the option of replicating from upstream
		if account.UpstreamPeerHostName != "" {
			a.tryReplicateBlob(w, r, *account, repoName, blobDigest)
		} else {
			keppel.ErrBlobUnknown.With("blob does not exist in this repository").WriteAsRegistryV2ResponseTo(w)
		}
		return
	}
	if respondWithError(w, err) {
		return
	}

	//prefer redirecting the client to a storage URL if the storage driver can give us one
	url, err := a.sd.URLForBlob(*account, blob.StorageID)
	if err == nil {
		w.Header().Set("Docker-Content-Digest", blob.Digest)
		w.Header().Set("Location", url)
		w.WriteHeader(http.StatusTemporaryRedirect)
		return
	}
	if err != keppel.ErrCannotGenerateURL {
		respondWithError(w, err)
		return
	}

	//return the blob contents to the client directly (NOTE: this code path is
	//rather lazy and esp. does not support range requests because it is only
	//used by unit tests anyway; all production-grade storage drivers have a
	//functional URLForBlob implementation)
	reader, lengthBytes, err := a.sd.ReadBlob(*account, blob.StorageID)
	if respondWithError(w, err) {
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Length", strconv.FormatUint(lengthBytes, 10))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Content-Digest", blob.Digest)
	w.WriteHeader(http.StatusOK)
	if r.Method != "HEAD" {
		_, err = io.Copy(w, reader)
		if err != nil {
			logg.Error("unexpected error from io.Copy() while sending blob to client: %s", err.Error())
		}
	}
}

func (a *API) tryReplicateBlob(w http.ResponseWriter, r *http.Request, account keppel.Account, repoName string, blobDigest digest.Digest) {
	repo, err := a.db.FindOrCreateRepository(repoName, account)
	if respondWithError(w, err) {
		return
	}

	repl := replication.NewReplicator(a.cfg, a.db, a.sd)
	blob := replication.Blob{
		Account: account,
		Repo:    *repo,
		Digest:  blobDigest,
	}
	responseWasWritten, err := repl.ReplicateBlob(blob, w, r.Method)

	if err != nil {
		if responseWasWritten {
			//we cannot write to `w` if br.Execute() wrote a response there already
			logg.Error("while trying to replicate blob %s in %s/%s: %s",
				blob.Digest, account.Name, repo.Name, err.Error())
		} else if err == replication.ErrConcurrentReplication {
			//special handling for GET during ongoing replication (429 Too Many
			//Requests is not a perfect match, but it's my best guess for getting
			//clients to automatically retry the request after a few seconds)
			w.Header().Set("Retry-After", "10")
			msg := "currently replicating on a different worker, please retry in a few seconds"
			keppel.ErrTooManyRequests.With(msg).WriteAsRegistryV2ResponseTo(w)
			return
		} else {
			respondWithError(w, err)
			return
		}
	}
}

//This implements the DELETE /v2/<account>/<repository>/blobs/<digest> endpoint.
func (a *API) handleDeleteBlob(w http.ResponseWriter, r *http.Request) {
	account, repoName, _ := a.checkAccountAccess(w, r)
	if account == nil {
		return
	}

	blobDigest, err := digest.Parse(mux.Vars(r)["digest"])
	if err != nil {
		keppel.ErrDigestInvalid.With(err.Error()).WriteAsRegistryV2ResponseTo(w)
		return
	}

	blob, err := a.db.FindBlobByRepositoryName(blobDigest, repoName, *account)
	if err == sql.ErrNoRows {
		keppel.ErrBlobUnknown.With("blob does not exist in this repository").WriteAsRegistryV2ResponseTo(w)
		return
	}
	if respondWithError(w, err) {
		return
	}

	//prepare the deletion in the DB (but don't commit until we've deleted in the StorageDriver)
	tx, err := a.db.Begin()
	if respondWithError(w, err) {
		return
	}
	defer keppel.RollbackUnlessCommitted(tx)

	//this also deletes all blob_mounts because of ON DELETE CASCADE
	_, err = tx.Delete(&blob)
	if respondWithError(w, err) {
		return
	}

	err = a.sd.DeleteBlob(*account, blob.StorageID)
	if respondWithError(w, err) {
		return
	}
	err = tx.Commit()
	if respondWithError(w, err) {
		return
	}

	w.Header().Set("Content-Length", "0")
	w.Header().Set("Docker-Content-Digest", blob.Digest)
	w.WriteHeader(http.StatusAccepted)
}
