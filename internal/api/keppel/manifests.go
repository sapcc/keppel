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
	"database/sql"
	"net/http"
	"regexp"
	"strings"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/respondwith"
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
	account := a.authenticateAccountScopedRequest(w, r, keppel.CanViewAccount)
	if account == nil {
		return
	}

	repoName := mux.Vars(r)["repo_name"]
	if !isValidRepoName(repoName) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	repo, err := a.db.FindRepository(repoName, *account)
	if err == sql.ErrNoRows {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if respondwith.ErrorText(w, err) {
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

var repoPathComponentRx = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

func isValidRepoName(name string) bool {
	if name == "" {
		return false
	}
	for _, pathComponent := range strings.Split(name, `/`) {
		if !repoPathComponentRx.MatchString(pathComponent) {
			return false
		}
	}
	return true
}
