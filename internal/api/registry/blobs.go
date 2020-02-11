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
	"strings"

	"github.com/gorilla/mux"
	"github.com/opencontainers/go-digest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sre"
	"github.com/sapcc/keppel/internal/api"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/replication"
)

//This implements the GET/HEAD /v2/<account>/<repository>/blobs/<digest> endpoint.
func (a *API) handleGetOrHeadBlob(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/v2/:account/:repo/blobs/:digest")
	account, repoName, token := a.checkAccountAccess(w, r)
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

	//before we branch into different code paths, count the pull
	if r.Method == "GET" {
		l := prometheus.Labels{"account": account.Name, "method": "registry-api"}
		if strings.HasPrefix(token.UserName, "replication@") {
			l["method"] = "replication"
		}
		api.BlobsPulledCounter.With(l).Inc()
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

	//if `err == nil && !responseWasWritten`, the blob was replicated by mounting
	//an existing blob with the same digest into this repo; in this case, we need
	//to restart the GET call to find the mounted blob
	if !responseWasWritten {
		//TODO ugly (and may cause an infinite loop if not handled carefully)
		a.handleGetOrHeadBlob(w, r)
	}
}

//This implements the DELETE /v2/<account>/<repository>/blobs/<digest> endpoint.
func (a *API) handleDeleteBlob(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/v2/:account/:repo/blobs/:digest")
	account, repoName, _ := a.checkAccountAccess(w, r)
	if account == nil {
		return
	}

	repo, err := a.db.FindRepository(repoName, *account)
	if err == sql.ErrNoRows {
		keppel.ErrNameUnknown.With("no such repository").WriteAsRegistryV2ResponseTo(w)
		return
	}
	if respondWithError(w, err) {
		return
	}

	blobDigest, err := digest.Parse(mux.Vars(r)["digest"])
	if err != nil {
		keppel.ErrDigestInvalid.With(err.Error()).WriteAsRegistryV2ResponseTo(w)
		return
	}

	blob, err := a.db.FindBlobByRepositoryID(blobDigest, repo.ID, *account)
	if err == sql.ErrNoRows {
		keppel.ErrBlobUnknown.With("blob does not exist in this repository").WriteAsRegistryV2ResponseTo(w)
		return
	}
	if respondWithError(w, err) {
		return
	}

	//unmount the blob from this particular repo (if it is mounted in other
	//repos, it will still be accessible there; otherwise keppel-janitor will
	//clean it up soon)
	_, err = a.db.Exec(`DELETE FROM blob_mounts WHERE blob_id = $1 AND repo_id = $2`, blob.ID, repo.ID)
	if respondWithError(w, err) {
		return
	}

	w.Header().Set("Content-Length", "0")
	w.Header().Set("Docker-Content-Digest", blob.Digest)
	w.WriteHeader(http.StatusAccepted)
}
