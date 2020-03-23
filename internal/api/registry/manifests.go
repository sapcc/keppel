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

	"github.com/gorilla/mux"
	"github.com/opencontainers/go-digest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/sre"
	"github.com/sapcc/keppel/internal/api"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/processor"
)

//This implements the HEAD/GET /v2/<repo>/manifests/<reference> endpoint.
func (a *API) handleGetOrHeadManifest(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/v2/:account/:repo/manifests/:reference")
	account, repo, _ := a.checkAccountAccess(w, r, createRepoIfMissingAndReplica)
	if account == nil {
		return
	}

	reference := keppel.ParseManifestReference(mux.Vars(r)["reference"])
	dbManifest, err := a.findManifestInDB(*account, *repo, reference)
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
			dbManifest, manifestBytes, err = a.processor().ReplicateManifest(*account, *repo, reference)
			if respondWithError(w, err) {
				return
			}
		} else {
			keppel.ErrManifestUnknown.With("").WithDetail(reference.Tag).WriteAsRegistryV2ResponseTo(w)
			return
		}
	} else {
		//if manifest was found in our DB, fetch the contents from the storage
		manifestBytes, err = a.sd.ReadManifest(*account, repo.Name, dbManifest.Digest)
		if respondWithError(w, err) {
			return
		}
	}

	//verify Accept header, if any
	if r.Header.Get("Accept") != "" {
		accepted := false
		for _, acceptHeader := range r.Header["Accept"] {
			for _, acceptField := range strings.Split(acceptHeader, ",") {
				acceptField = strings.SplitN(acceptField, ";", 2)[0]
				acceptField = strings.TrimSpace(acceptField)
				if acceptField == dbManifest.MediaType || acceptField == "*/*" { // Accept: */* is used by curl(1)
					accepted = true
				}
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

	//count the pull
	if r.Method == "GET" {
		l := prometheus.Labels{"account": account.Name, "auth_tenant_id": account.AuthTenantID, "method": "registry-api"}
		api.ManifestsPulledCounter.With(l).Inc()
	}
}

func (a *API) findManifestInDB(account keppel.Account, repo keppel.Repository, reference keppel.ManifestReference) (*keppel.Manifest, error) {
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
	err := a.db.SelectOne(&dbManifest,
		`SELECT * FROM manifests WHERE repo_id = $1 AND digest = $2`,
		repo.ID, refDigest.String(),
	)
	return &dbManifest, err
}

//This implements the DELETE /v2/<repo>/manifests/<reference> endpoint.
func (a *API) handleDeleteManifest(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/v2/:account/:repo/manifests/:reference")
	account, repo, _ := a.checkAccountAccess(w, r, failIfRepoMissing)
	if account == nil {
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

	//delete manifest from the database
	result, err := a.db.Exec(
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

	//delete the manifest in the backend
	err = a.sd.DeleteManifest(*account, repo.Name, digest.String())
	if respondWithError(w, err) {
		return
	}
	//^ NOTE: We do this *after* the deletion is durable in the DB to be extra
	//sure that we did not break any constraints (esp. manifest-manifest refs and
	//manifest-blob refs) that the DB enforces. Doing things in this order might
	//mean that, if DeleteManifest fails, we're left with a manifest in the
	//backing storage that is not referenced in the DB anymore, but this is not a
	//huge problem since the janitor can clean those up after the fact. What's
	//most important is that we don't lose any data in the backing storage while
	//it is still referenced in the DB.

	w.WriteHeader(http.StatusAccepted)
}

//This implements the PUT /v2/<repo>/manifests/<reference> endpoint.
func (a *API) handlePutManifest(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/v2/:account/:repo/manifests/:reference")
	account, repo, _ := a.checkAccountAccess(w, r, createRepoIfMissing)
	if account == nil {
		return
	}

	//forbid pushing into replica accounts
	if account.UpstreamPeerHostName != "" {
		msg := fmt.Sprintf("cannot push into replica account (push to %s/%s/%s instead!)",
			account.UpstreamPeerHostName, account.Name, repo.Name,
		)
		keppel.ErrUnsupported.With(msg).WithStatus(http.StatusMethodNotAllowed).WriteAsRegistryV2ResponseTo(w)
		return
	}

	//read manifest from request
	manifestBytes, err := ioutil.ReadAll(r.Body)
	if respondWithError(w, err) {
		return
	}

	//validate and store manifest
	manifest, err := a.processor().ValidateAndStoreManifest(*account, *repo, processor.IncomingManifest{
		Reference: keppel.ParseManifestReference(mux.Vars(r)["reference"]),
		MediaType: r.Header.Get("Content-Type"),
		Contents:  manifestBytes,
		PushedAt:  a.timeNow(),
	})
	if respondWithError(w, err) {
		return
	}

	//count the push
	l := prometheus.Labels{"account": account.Name, "auth_tenant_id": account.AuthTenantID, "method": "registry-api"}
	api.ManifestsPushedCounter.With(l).Inc()

	w.Header().Set("Content-Length", "0")
	w.Header().Set("Docker-Content-Digest", manifest.Digest)
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/manifests/%s", repo.FullName(), manifest.Digest))
	w.WriteHeader(http.StatusCreated)
}
