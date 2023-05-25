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
	"time"

	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/keppel"
)

// Repository represents a repository in the API.
type Repository struct {
	Name          string `json:"name"`
	ManifestCount uint64 `json:"manifest_count"`
	TagCount      uint64 `json:"tag_count"`
	SizeBytes     uint64 `json:"size_bytes,omitempty"`
	PushedAt      int64  `json:"pushed_at,omitempty"`
}

var repositoryGetQuery = sqlext.SimplifyWhitespace(`
	WITH
		blob_stats AS (
			SELECT bm.repo_id AS repo_id, SUM(b.size_bytes) AS size_bytes
			  FROM blob_mounts bm
			  JOIN blobs b ON b.id = bm.blob_id
			 GROUP BY bm.repo_id
		),
		manifest_stats AS (
			SELECT repo_id, COUNT(*) AS count, MAX(pushed_at) AS pushed_at
			  FROM manifests
			 GROUP BY repo_id
		),
		tag_stats AS (
			SELECT repo_id, COUNT(*) AS count, MAX(pushed_at) AS pushed_at
			  FROM tags
			 GROUP BY repo_id
		)
	SELECT r.name,
	       bs.size_bytes,
	       ms.count, ms.pushed_at,
	       ts.count, ts.pushed_at
	  FROM repos r
	  LEFT OUTER JOIN blob_stats     bs ON r.id = bs.repo_id
	  LEFT OUTER JOIN manifest_stats ms ON r.id = ms.repo_id
	  LEFT OUTER JOIN tag_stats      ts ON r.id = ts.repo_id
	 WHERE r.account_name = $1 AND $CONDITION
	 ORDER BY name ASC
	 LIMIT $LIMIT
`)

func (a *API) handleGetRepositories(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/accounts/:account/repositories")
	authz := a.authenticateRequest(w, r, accountScopeFromRequest(r, keppel.CanViewAccount))
	if authz == nil {
		return
	}
	account := a.findAccountFromRequest(w, r, authz)
	if account == nil {
		return
	}

	query, bindValues, limit, err := paginatedQuery{
		SQL:         repositoryGetQuery,
		MarkerField: "r.name",
		Options:     r.URL.Query(),
		BindValues:  []interface{}{account.Name},
	}.Prepare()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var result struct {
		Repos       []Repository `json:"repositories"`
		IsTruncated bool         `json:"truncated,omitempty"`
	}
	err = sqlext.ForeachRow(a.db, query, bindValues, func(rows *sql.Rows) error {
		var (
			name                string
			sizeBytes           *uint64
			manifestCount       *uint64
			maxManifestPushedAt *time.Time
			tagCount            *uint64
			maxTagPushedAt      *time.Time
		)
		err := rows.Scan(
			&name,
			&sizeBytes,
			&manifestCount, &maxManifestPushedAt,
			&tagCount, &maxTagPushedAt,
		)
		if err == nil {
			result.Repos = append(result.Repos, Repository{
				Name:          name,
				ManifestCount: unpackUint64OrZero(manifestCount),
				TagCount:      unpackUint64OrZero(tagCount),
				SizeBytes:     unpackUint64OrZero(sizeBytes),
				PushedAt:      maxTimeToUnix(maxTagPushedAt, maxManifestPushedAt),
			})
		}
		return err
	})
	if respondwith.ErrorText(w, err) {
		return
	}

	if result.Repos == nil {
		result.Repos = []Repository{}
	}
	if uint64(len(result.Repos)) > limit {
		result.Repos = result.Repos[0:limit]
		result.IsTruncated = true
	}
	respondwith.JSON(w, http.StatusOK, result)
}

func unpackUint64OrZero(x *uint64) uint64 {
	if x == nil {
		return 0
	}
	return *x
}

// Returns the Unix timestamp corresponding to the later of the input times (or
// 0 if both are nil).
func maxTimeToUnix(x, y *time.Time) int64 {
	val := int64(0)
	if x != nil {
		val = x.Unix()
	}
	if y != nil {
		if val < y.Unix() {
			val = y.Unix()
		}
	}
	return val
}

func (a *API) handleDeleteRepository(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/accounts/:account/repositories/:repo")
	authz := a.authenticateRequest(w, r, repoScopeFromRequest(r, keppel.CanDeleteFromAccount))
	if authz == nil {
		return
	}
	account := a.findAccountFromRequest(w, r, authz)
	if account == nil {
		return
	}
	repo := a.findRepositoryFromRequest(w, r, *account)
	if repo == nil {
		return
	}

	tx, err := a.db.Begin()
	if respondwith.ErrorText(w, err) {
		return
	}
	defer sqlext.RollbackUnlessCommitted(tx)

	//deleting a repo is only allowed if there is nothing in it
	manifestCount, err := tx.SelectInt(
		`SELECT COUNT(*) FROM manifests WHERE repo_id = $1`,
		repo.ID,
	)
	if respondwith.ErrorText(w, err) {
		return
	}
	if manifestCount > 0 {
		msg := "cannot delete repository while there are still manifests in it"
		http.Error(w, msg, http.StatusConflict)
		return
	}

	uploadCount, err := tx.SelectInt(`SELECT COUNT(*) FROM uploads WHERE repo_id = $1`, repo.ID)
	if respondwith.ErrorText(w, err) {
		return
	}
	if uploadCount > 0 {
		msg := "cannot delete repository while blobs in it are being uploaded"
		http.Error(w, msg, http.StatusConflict)
		return
	}
	//^ NOTE: It's not a problem if there are blob_mounts in this repo. When the
	//repo is deleted, its blob mounts will be deleted as well, and the janitor
	//will then clean up any blobs without any remaining mounts.

	_, err = tx.Delete(repo)
	if err == nil {
		err = tx.Commit()
	}
	if respondwith.ErrorText(w, err) {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
