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

package keppelv1

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
)

//Manifest represents a manifest in the API.
type Manifest struct {
	Digest    string `json:"digest"`
	MediaType string `json:"media_type"`
	SizeBytes uint64 `json:"size_bytes"`
	PushedAt  int64  `json:"pushed_at"`
	Tags      []Tag  `json:"tags,omitempty"`
}

//Tag represents a tag in the API.
type Tag struct {
	Name     string `json:"name"`
	PushedAt int64  `json:"pushed_at"`
}

var manifestGetQuery = `
	SELECT *
	  FROM manifests
	 WHERE repo_id = $1 AND $CONDITION
	 ORDER BY digest ASC
	 LIMIT $LIMIT
`

var tagGetQuery = `
	SELECT *
	  FROM tags
	 WHERE repo_id = $1 AND digest >= $2 AND digest <= $3
`

func (a *API) handleGetManifests(w http.ResponseWriter, r *http.Request) {
	account, _ := a.authenticateAccountScopedRequest(w, r, keppel.CanViewAccount)
	if account == nil {
		return
	}
	repo := a.findRepositoryFromRequest(w, r, *account)
	if repo == nil {
		return
	}

	query, bindValues, limit, err := paginatedQuery{
		SQL:         manifestGetQuery,
		MarkerField: "digest",
		Options:     r.URL.Query(),
		BindValues:  []interface{}{repo.ID},
	}.Prepare()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var dbManifests []keppel.Manifest
	_, err = a.db.Select(&dbManifests, query, bindValues...)
	if respondwith.ErrorText(w, err) {
		return
	}

	var result struct {
		Manifests   []*Manifest `json:"manifests"`
		IsTruncated bool        `json:"truncated,omitempty"`
	}
	for _, dbManifest := range dbManifests {
		if uint64(len(result.Manifests)) >= limit {
			result.IsTruncated = true
			break
		}
		result.Manifests = append(result.Manifests, &Manifest{
			Digest:    dbManifest.Digest,
			MediaType: dbManifest.MediaType,
			SizeBytes: dbManifest.SizeBytes,
			PushedAt:  dbManifest.PushedAt.Unix(),
		})
	}

	//since results were retrieved in sorted order, we know that for each
	//manifest in the result set, its digest is >= the first digest and <= the
	//last digest
	firstDigest := result.Manifests[0].Digest
	lastDigest := result.Manifests[len(result.Manifests)-1].Digest
	var dbTags []keppel.Tag
	_, err = a.db.Select(&dbTags, tagGetQuery, repo.ID, firstDigest, lastDigest)
	if respondwith.ErrorText(w, err) {
		return
	}

	tagsByDigest := make(map[string][]Tag)
	for _, dbTag := range dbTags {
		tagsByDigest[dbTag.Digest] = append(tagsByDigest[dbTag.Digest], Tag{
			Name:     dbTag.Name,
			PushedAt: dbTag.PushedAt.Unix(),
		})
	}
	for _, manifest := range result.Manifests {
		manifest.Tags = tagsByDigest[manifest.Digest]
	}

	respondwith.JSON(w, http.StatusOK, result)
}

func (a *API) handleDeleteManifest(w http.ResponseWriter, r *http.Request) {
	account, authz := a.authenticateAccountScopedRequest(w, r, keppel.CanDeleteFromAccount)
	if account == nil {
		return
	}
	repo := a.findRepositoryFromRequest(w, r, *account)
	if repo == nil {
		return
	}
	digest, err := digest.Parse(mux.Vars(r)["digest"])
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
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
	tokenForBackend, err := auth.Token{
		UserName: authz.UserName(),
		Audience: a.cfg.APIPublicHostname(),
		Access: []auth.Scope{{
			ResourceType: "repository",
			ResourceName: repo.FullName(),
			Actions:      []string{"delete"},
		}},
	}.Issue(a.cfg)
	if respondwith.ErrorText(w, err) {
		return
	}
	reqPath := fmt.Sprintf("/v2/%s/manifests/%s", repo.FullName(), digest)
	req, err := http.NewRequest("DELETE", reqPath, nil)
	if respondwith.ErrorText(w, err) {
		return
	}
	req.Header.Set("Authorization", "Bearer "+tokenForBackend.SignedToken)
	resp, err := a.orchDriver.DoHTTPRequest(*account, req, keppel.FollowRedirects)
	if respondwith.ErrorText(w, err) {
		return
	}
	if resp.StatusCode != http.StatusAccepted {
		msg := fmt.Sprintf(
			"expected backend request to return status code %d, got %s instead",
			http.StatusAccepted, resp.Status,
		)
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}

	err = tx.Commit()
	if respondwith.ErrorText(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
