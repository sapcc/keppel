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
	"encoding/json"
	"net/http"
	"sort"

	"github.com/gorilla/mux"
	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/go-bits/sre"
	"github.com/sapcc/keppel/internal/clair"
	"github.com/sapcc/keppel/internal/keppel"
)

//Manifest represents a manifest in the API.
type Manifest struct {
	Digest                        string                    `json:"digest"`
	MediaType                     string                    `json:"media_type"`
	SizeBytes                     uint64                    `json:"size_bytes"`
	PushedAt                      int64                     `json:"pushed_at"`
	LastPulledAt                  *int64                    `json:"last_pulled_at"`
	Tags                          []Tag                     `json:"tags,omitempty"`
	LabelsJSON                    json.RawMessage           `json:"labels,omitempty"`
	GCStatusJSON                  json.RawMessage           `json:"gc_status,omitempty"`
	VulnerabilityStatus           clair.VulnerabilityStatus `json:"vulnerability_status"`
	VulnerabilityScanErrorMessage string                    `json:"vulnerability_scan_error,omitempty"`
	MinLayerCreatedAt             *int64                    `json:"min_layer_created_at"`
	MaxLayerCreatedAt             *int64                    `json:"max_layer_created_at"`
}

//Tag represents a tag in the API.
type Tag struct {
	Name         string `json:"name"`
	PushedAt     int64  `json:"pushed_at"`
	LastPulledAt *int64 `json:"last_pulled_at"`
}

var manifestGetQuery = keppel.SimplifyWhitespaceInSQL(`
	SELECT *
	  FROM manifests
	 WHERE repo_id = $1 AND $CONDITION
	 ORDER BY digest ASC
	 LIMIT $LIMIT
`)

var tagGetQuery = keppel.SimplifyWhitespaceInSQL(`
	SELECT *
	  FROM tags
	 WHERE repo_id = $1 AND digest >= $2 AND digest <= $3
`)

func (a *API) handleGetManifests(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/keppel/v1/accounts/:account/repositories/:repo/_manifests")
	authz := a.authenticateRequest(w, r, repoScopeFromRequest(r, keppel.CanPullFromAccount))
	if authz == nil {
		return
	}
	account := a.findAccountFromRequest(w, r)
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
			Digest:                        dbManifest.Digest,
			MediaType:                     dbManifest.MediaType,
			SizeBytes:                     dbManifest.SizeBytes,
			PushedAt:                      dbManifest.PushedAt.Unix(),
			LastPulledAt:                  keppel.MaybeTimeToUnix(dbManifest.LastPulledAt),
			LabelsJSON:                    json.RawMessage(dbManifest.LabelsJSON),
			GCStatusJSON:                  json.RawMessage(dbManifest.GCStatusJSON),
			VulnerabilityStatus:           dbManifest.VulnerabilityStatus,
			VulnerabilityScanErrorMessage: dbManifest.VulnerabilityScanErrorMessage,
			MinLayerCreatedAt:             keppel.MaybeTimeToUnix(dbManifest.MinLayerCreatedAt),
			MaxLayerCreatedAt:             keppel.MaybeTimeToUnix(dbManifest.MaxLayerCreatedAt),
		})
	}

	if len(result.Manifests) == 0 {
		result.Manifests = []*Manifest{}
	} else {
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
				Name:         dbTag.Name,
				PushedAt:     dbTag.PushedAt.Unix(),
				LastPulledAt: keppel.MaybeTimeToUnix(dbTag.LastPulledAt),
			})
		}
		for _, manifest := range result.Manifests {
			manifest.Tags = tagsByDigest[manifest.Digest]
			//sort in deterministic order for unit test
			sort.Slice(manifest.Tags, func(i, j int) bool {
				return manifest.Tags[i].Name < manifest.Tags[j].Name
			})
		}
	}

	respondwith.JSON(w, http.StatusOK, result)
}

func (a *API) handleDeleteManifest(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/keppel/v1/accounts/:account/repositories/:repo/_manifests/:digest")
	authz := a.authenticateRequest(w, r, repoScopeFromRequest(r, keppel.CanDeleteFromAccount))
	if authz == nil {
		return
	}
	account := a.findAccountFromRequest(w, r)
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

	err = a.processor().DeleteManifest(*account, *repo, digest.String(), keppel.AuditContext{
		UserIdentity: authz.UserIdentity,
		Request:      r,
	})
	if err == sql.ErrNoRows {
		http.Error(w, "no such manifest", http.StatusNotFound)
		return
	}
	if respondwith.ErrorText(w, err) {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/keppel/v1/accounts/:account/repositories/:repo/_tags/:name")
	authz := a.authenticateRequest(w, r, repoScopeFromRequest(r, keppel.CanDeleteFromAccount))
	if authz == nil {
		return
	}
	account := a.findAccountFromRequest(w, r)
	if account == nil {
		return
	}
	repo := a.findRepositoryFromRequest(w, r, *account)
	if repo == nil {
		return
	}
	tagName := mux.Vars(r)["tag_name"]

	err := a.processor().DeleteTag(*account, *repo, tagName, keppel.AuditContext{
		UserIdentity: authz.UserIdentity,
		Request:      r,
	})
	if err == sql.ErrNoRows {
		http.Error(w, "no such tag", http.StatusNotFound)
		return
	}
	if respondwith.ErrorText(w, err) {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleGetVulnerabilityReport(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/keppel/v1/accounts/:account/repositories/:repo/_manifests/:digest/vulnerability_report")
	authz := a.authenticateRequest(w, r, repoScopeFromRequest(r, keppel.CanPullFromAccount))
	if authz == nil {
		return
	}
	account := a.findAccountFromRequest(w, r)
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

	manifest, err := keppel.FindManifest(a.db, *repo, digest.String())
	if err == sql.ErrNoRows {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if respondwith.ErrorText(w, err) {
		return
	}

	//there is no vulnerability report if:
	//- we don't have vulnerability scanning enabled at all
	//- vulnerability scanning is not done yet
	//- the image does not have any blobs that could be scanned for vulnerabilities
	blobCount, err := a.db.SelectInt(
		`SELECT COUNT(*) FROM manifest_blob_refs WHERE repo_id = $1 AND digest = $2`,
		repo.ID, manifest.Digest,
	)
	if respondwith.ErrorText(w, err) {
		return
	}
	if a.cfg.ClairClient == nil || !manifest.VulnerabilityStatus.HasReport() || blobCount == 0 {
		http.Error(w, "no vulnerability report found", http.StatusMethodNotAllowed)
		return
	}

	clairReport, err := a.cfg.ClairClient.GetVulnerabilityReport(manifest.Digest)
	if respondwith.ErrorText(w, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, clairReport)
}
