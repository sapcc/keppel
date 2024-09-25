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
	"errors"
	"fmt"
	"html"
	"net/http"
	"sort"

	"github.com/gorilla/mux"
	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/errext"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/api"
	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// Manifest represents a manifest in the API.
type Manifest struct {
	Digest                        digest.Digest              `json:"digest"`
	MediaType                     string                     `json:"media_type"`
	SizeBytes                     uint64                     `json:"size_bytes"`
	PushedAt                      int64                      `json:"pushed_at"`
	LastPulledAt                  *int64                     `json:"last_pulled_at"`
	Tags                          []Tag                      `json:"tags,omitempty"`
	LabelsJSON                    json.RawMessage            `json:"labels,omitempty"`
	GCStatusJSON                  json.RawMessage            `json:"gc_status,omitempty"`
	VulnerabilityStatus           models.VulnerabilityStatus `json:"vulnerability_status"`
	VulnerabilityScanErrorMessage string                     `json:"vulnerability_scan_error,omitempty"`
	MinLayerCreatedAt             *int64                     `json:"min_layer_created_at"`
	MaxLayerCreatedAt             *int64                     `json:"max_layer_created_at"`
}

// Tag represents a tag in the API.
type Tag struct {
	Name         string `json:"name"`
	PushedAt     int64  `json:"pushed_at"`
	LastPulledAt *int64 `json:"last_pulled_at"`
}

var manifestGetQuery = sqlext.SimplifyWhitespace(`
	SELECT *
	  FROM manifests
	 WHERE repo_id = $1 AND $CONDITION
	 ORDER BY digest ASC
	 LIMIT $LIMIT
`)

var securityInfoGetQuery = sqlext.SimplifyWhitespace(`
	SELECT * FROM trivy_security_info
	WHERE repo_id = $1 AND $CONDITION
	ORDER BY digest ASC
	LIMIT $LIMIT
`)

var tagGetQuery = sqlext.SimplifyWhitespace(`
	SELECT *
	  FROM tags
	 WHERE repo_id = $1 AND digest >= $2 AND digest <= $3
`)

func (a *API) handleGetManifests(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/accounts/:account/repositories/:repo/_manifests")
	authz := a.authenticateRequest(w, r, repoScopeFromRequest(r, keppel.CanPullFromAccount))
	if authz == nil {
		return
	}
	account := a.findAccountFromRequest(w, r, authz)
	if account == nil {
		return
	}
	repo := a.findRepositoryFromRequest(w, r, account.Name)
	if repo == nil {
		return
	}

	manifestQuery, vulnBindValues, manifestLimit, err := paginatedQuery{
		SQL:         manifestGetQuery,
		MarkerField: "digest",
		Options:     r.URL.Query(),
		BindValues:  []any{repo.ID},
	}.Prepare()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var dbManifests []models.Manifest
	_, err = a.db.Select(&dbManifests, manifestQuery, vulnBindValues...)
	if respondwith.ErrorText(w, err) {
		return
	}

	securityInfoQuery, securityBindValues, _, err := paginatedQuery{
		SQL:         securityInfoGetQuery,
		MarkerField: "digest",
		Options:     r.URL.Query(),
		BindValues:  []any{repo.ID},
	}.Prepare()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var dbSecurityInfos []models.TrivySecurityInfo
	_, err = a.db.Select(&dbSecurityInfos, securityInfoQuery, securityBindValues...)
	if respondwith.ErrorText(w, err) {
		return
	}

	securityInfos := make(map[digest.Digest]models.TrivySecurityInfo, len(dbSecurityInfos))
	for _, securityInfo := range dbSecurityInfos {
		securityInfos[securityInfo.Digest] = securityInfo
	}

	var result struct {
		Manifests   []*Manifest `json:"manifests"`
		IsTruncated bool        `json:"truncated,omitempty"`
	}
	for _, dbManifest := range dbManifests {
		if uint64(len(result.Manifests)) >= manifestLimit {
			result.IsTruncated = true
			break
		}

		var (
			securityInfo models.TrivySecurityInfo
			ok           bool
		)
		if securityInfo, ok = securityInfos[dbManifest.Digest]; !ok {
			http.Error(w, fmt.Sprintf("missing trivy vulnerability report for digest %s", dbManifest.Digest), http.StatusInternalServerError)
			return
		}

		result.Manifests = append(result.Manifests, &Manifest{
			Digest:                        dbManifest.Digest,
			MediaType:                     dbManifest.MediaType,
			SizeBytes:                     dbManifest.SizeBytes,
			PushedAt:                      dbManifest.PushedAt.Unix(),
			LastPulledAt:                  keppel.MaybeTimeToUnix(dbManifest.LastPulledAt),
			LabelsJSON:                    json.RawMessage(dbManifest.LabelsJSON),
			GCStatusJSON:                  json.RawMessage(dbManifest.GCStatusJSON),
			VulnerabilityStatus:           securityInfo.VulnerabilityStatus,
			VulnerabilityScanErrorMessage: securityInfo.Message,
			MinLayerCreatedAt:             keppel.MaybeTimeToUnix(dbManifest.MinLayerCreatedAt),
			MaxLayerCreatedAt:             keppel.MaybeTimeToUnix(dbManifest.MaxLayerCreatedAt),
		})
	}

	if len(result.Manifests) == 0 {
		result.Manifests = []*Manifest{}
	} else {
		// since results were retrieved in sorted order, we know that for each
		// manifest in the result set, its digest is >= the first digest and <= the
		// last digest
		firstDigest := result.Manifests[0].Digest
		lastDigest := result.Manifests[len(result.Manifests)-1].Digest
		var dbTags []models.Tag
		_, err = a.db.Select(&dbTags, tagGetQuery, repo.ID, firstDigest, lastDigest)
		if respondwith.ErrorText(w, err) {
			return
		}

		tagsByDigest := make(map[digest.Digest][]Tag)
		for _, dbTag := range dbTags {
			tagsByDigest[dbTag.Digest] = append(tagsByDigest[dbTag.Digest], Tag{
				Name:         dbTag.Name,
				PushedAt:     dbTag.PushedAt.Unix(),
				LastPulledAt: keppel.MaybeTimeToUnix(dbTag.LastPulledAt),
			})
		}
		for _, manifest := range result.Manifests {
			manifest.Tags = tagsByDigest[manifest.Digest]
			// sort in deterministic order for unit test
			sort.Slice(manifest.Tags, func(i, j int) bool {
				return manifest.Tags[i].Name < manifest.Tags[j].Name
			})
		}
	}

	respondwith.JSON(w, http.StatusOK, result)
}

func (a *API) handleDeleteManifest(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/accounts/:account/repositories/:repo/_manifests/:digest")
	authz := a.authenticateRequest(w, r, repoScopeFromRequest(r, keppel.CanDeleteFromAccount))
	if authz == nil {
		return
	}
	account := a.findAccountFromRequest(w, r, authz)
	if account == nil {
		return
	}
	repo := a.findRepositoryFromRequest(w, r, account.Name)
	if repo == nil {
		return
	}
	parsedDigest, err := digest.Parse(mux.Vars(r)["digest"])
	if err != nil {
		http.Error(w, "digest not found", http.StatusNotFound)
		return
	}

	err = a.processor().DeleteManifest(r.Context(), account.Reduced(), *repo, parsedDigest, keppel.AuditContext{
		UserIdentity: authz.UserIdentity,
		Request:      r,
	})
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "no such manifest", http.StatusNotFound)
		return
	}
	if respondwith.ErrorText(w, err) {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/accounts/:account/repositories/:repo/_tags/:name")
	authz := a.authenticateRequest(w, r, repoScopeFromRequest(r, keppel.CanDeleteFromAccount))
	if authz == nil {
		return
	}
	account := a.findAccountFromRequest(w, r, authz)
	if account == nil {
		return
	}
	repo := a.findRepositoryFromRequest(w, r, account.Name)
	if repo == nil {
		return
	}
	tagName := mux.Vars(r)["tag_name"]

	err := a.processor().DeleteTag(account.Reduced(), *repo, tagName, keppel.AuditContext{
		UserIdentity: authz.UserIdentity,
		Request:      r,
	})
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "no such tag", http.StatusNotFound)
		return
	}
	if respondwith.ErrorText(w, err) {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleGetTrivyReport(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/accounts/:account/repositories/:repo/_manifests/:digest/trivy_report")
	authz := a.authenticateRequest(w, r, repoScopeFromRequest(r, keppel.CanPullFromAccount))
	if authz == nil {
		return
	}
	account := a.findAccountFromRequest(w, r, authz)
	if account == nil {
		return
	}

	err := api.CheckRateLimit(r, a.rle, account.Reduced(), authz, keppel.TrivyReportRetrieveAction, 1)
	if err != nil {
		if rerr, ok := errext.As[*keppel.RegistryV2Error](err); ok && rerr != nil {
			rerr.WriteAsRegistryV2ResponseTo(w, r)
			return
		} else if respondwith.ErrorText(w, err) {
			return
		}
	}

	repo := a.findRepositoryFromRequest(w, r, account.Name)
	if repo == nil {
		return
	}
	parsedDigest, err := digest.Parse(mux.Vars(r)["digest"])
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	manifest, err := keppel.FindManifest(a.db, *repo, parsedDigest)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if respondwith.ErrorText(w, err) {
		return
	}

	securityInfo, err := keppel.GetSecurityInfo(a.db, repo.ID, parsedDigest)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if respondwith.ErrorText(w, err) {
		return
	}

	// there is no vulnerability report if:
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
	if a.cfg.Trivy == nil || !securityInfo.VulnerabilityStatus.HasReport() || blobCount == 0 {
		http.Error(w, "no vulnerability report found", http.StatusMethodNotAllowed)
		return
	}

	imageRef := models.ImageReference{
		Host:      a.cfg.APIPublicHostname,
		RepoName:  fmt.Sprintf("%s/%s", account.Name, repo.Name),
		Reference: models.ManifestReference{Digest: manifest.Digest},
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	if format != "json" && format != "spdx-json" {
		http.Error(w, fmt.Sprintf("format %s not supported", html.EscapeString(format)), http.StatusBadRequest)
		return
	}

	tokenResp, err := auth.IssueTokenForTrivy(a.cfg, repo.FullName())
	if respondwith.ErrorText(w, err) {
		return
	}

	report, err := a.cfg.Trivy.ScanManifest(r.Context(), tokenResp.Token, imageRef, format)
	if respondwith.ErrorText(w, err) {
		return
	}

	relevantPolicies, err := keppel.GetSecurityScanPolicies(*account, *repo)
	if respondwith.ErrorText(w, err) {
		return
	}
	err = relevantPolicies.EnrichReport(&report)
	if respondwith.ErrorText(w, err) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(report.Contents)
}
