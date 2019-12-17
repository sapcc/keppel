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
	"fmt"
	"net/http"

	"github.com/docker/distribution"
	"github.com/gorilla/mux"
	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/keppel/internal/keppel"

	//distribution.UnmarshalManifest() relies on the following packages
	//registering their manifest schemas.
	_ "github.com/docker/distribution/manifest/manifestlist"
	_ "github.com/docker/distribution/manifest/ocischema"
	_ "github.com/docker/distribution/manifest/schema1"
	_ "github.com/docker/distribution/manifest/schema2"
)

//This implements the HEAD/GET/PUT/DELETE /v2/<repo>/manifests/<reference> endpoint.
func (a *API) handleManifest(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "DELETE":
		a.handleDeleteManifest(w, r)
	case "PUT":
		a.handlePutManifest(w, r)
	default:
		a.handleProxyToAccount(w, r)
	}
}

//This implements the DELETE /v2/<repo>/manifests/<reference> endpoint.
func (a *API) handleDeleteManifest(w http.ResponseWriter, r *http.Request) {
	account, repoName := a.checkAccountAccess(w, r)
	if account == nil {
		return
	}
	repo, err := a.db.FindOrCreateRepository(repoName, *account)
	if respondwith.ErrorText(w, err) {
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
	if respondwith.ErrorText(w, err) {
		return
	}
	defer keppel.RollbackUnlessCommitted(tx)
	result, err := a.db.Exec(
		//this also deletes tags referencing this manifest because of "ON DELETE CASCADE"
		`DELETE FROM manifests WHERE repo_id = $1 AND digest = $2`,
		repo.ID, digest)
	if respondwith.ErrorText(w, err) {
		return
	}
	rowsDeleted, err := result.RowsAffected()
	if respondwith.ErrorText(w, err) {
		return
	}
	if rowsDeleted == 0 {
		keppel.ErrManifestUnknown.With("no such manifest").WriteAsRegistryV2ResponseTo(w)
		return
	}

	//DELETE the manifest in the backend
	resp := a.proxyRequestToRegistry(w, r, *account)
	if resp == nil {
		return
	}
	err = tx.Commit()
	if respondwith.ErrorText(w, err) {
		return
	}

	a.proxyResponseToCaller(w, resp)
}

//This implements the PUT /v2/<repo>/manifests/<reference> endpoint.
func (a *API) handlePutManifest(w http.ResponseWriter, r *http.Request) {
	account, repoName := a.checkAccountAccess(w, r)
	if account == nil {
		return
	}

	//check if user has enough quota to push a manifest
	quotas, err := a.db.FindQuotas(account.AuthTenantID)
	if respondwith.ErrorText(w, err) {
		return
	}
	if quotas == nil {
		quotas = keppel.DefaultQuotas(account.AuthTenantID)
	}
	manifestUsage, err := quotas.GetManifestUsage(a.db)
	if respondwith.ErrorText(w, err) {
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
	if respondwith.ErrorText(w, err) {
		return
	}

	reference := mux.Vars(r)["reference"]
	//if `reference` parses as a digest, interpret it as a digest, otherwise
	//interpret it as a tag name
	digestFromReference, err := digest.Parse(reference)
	pushesToTag := err != nil

	//validate manifest on our side
	reqBuf, ok := a.interceptRequestBody(w, r)
	if !ok {
		return
	}
	manifest, manifestDesc, err := distribution.UnmarshalManifest(r.Header.Get("Content-Type"), reqBuf)
	if err != nil {
		keppel.ErrManifestInvalid.With(err.Error()).WriteAsRegistryV2ResponseTo(w)
		return
	}

	//if <reference> is not a tag, it must be the digest of the manifest
	if !pushesToTag && manifestDesc.Digest != digestFromReference {
		keppel.ErrDigestInvalid.With("actual manifest digest is " + manifestDesc.Digest.String()).WriteAsRegistryV2ResponseTo(w)
		return
	}

	//compute total size of image
	sizeBytes := uint64(manifestDesc.Size)
	for _, desc := range manifest.References() {
		sizeBytes += uint64(desc.Size)
	}

	//prepare new database entries, so that we only have to commit the transaction once the backend PUT is successful
	tx, err := a.db.Begin()
	if respondwith.ErrorText(w, err) {
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
	if respondwith.ErrorText(w, err) {
		return
	}
	if pushesToTag {
		err = keppel.Tag{
			RepositoryID: repo.ID,
			Name:         reference,
			Digest:       manifestDesc.Digest.String(),
			PushedAt:     a.timeNow(),
		}.InsertIfMissing(tx)
		if respondwith.ErrorText(w, err) {
			return
		}
	}

	//PUT the manifest in the backend
	resp := a.proxyRequestToRegistry(w, r, *account)
	if resp == nil {
		return
	}
	err = tx.Commit()
	if respondwith.ErrorText(w, err) {
		return
	}

	a.proxyResponseToCaller(w, resp)
}
