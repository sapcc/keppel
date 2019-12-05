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
	"strconv"
	"strings"

	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/keppel/internal/keppel"
)

//Repository represents a repository in the API.
type Repository struct {
	Name          string `json:"name"`
	ManifestCount uint64 `json:"manifest_count"`
	TagCount      uint64 `json:"tag_count"`
}

var repositoryGetQuery = `
	SELECT r.name,
	       (SELECT COUNT(*) FROM manifests WHERE repo_id = r.id),
	       (SELECT COUNT(*) FROM tags WHERE repo_id = r.id)
	  FROM repos r
	 WHERE r.account_name = $1 AND $CONDITION
	 ORDER BY name ASC
	 LIMIT $LIMIT
`

func (a *API) handleGetRepositories(w http.ResponseWriter, r *http.Request) {
	account := a.authenticateAccountScopedRequest(w, r, keppel.CanViewAccount)
	if account == nil {
		return
	}

	query := repositoryGetQuery
	bindValues := []interface{}{account.Name}

	marker := r.URL.Query().Get("marker")
	if marker == "" {
		query = strings.Replace(query, `$CONDITION`, `TRUE`, 1)
	} else {
		query = strings.Replace(query, `$CONDITION`, `name > $2`, 1)
		bindValues = append(bindValues, marker)
	}

	//hidden feature: allow lowering the default limit with ?limit= (we only
	//really use this for the unit tests)
	limit := uint64(1000)
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limitVal, err := strconv.ParseUint(limitStr, 10, 64)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if limitVal < limit { //never allow more than 1000 results at once
			limit = limitVal
		}
	}
	//fetch one more than `limit`: otherwise we cannot distinguish between a
	//truncated 1000-row result and a non-truncated 1000-row result
	query = strings.Replace(query, `$LIMIT`, strconv.FormatUint(limit+1, 10), 1)

	var result struct {
		Repos       []Repository `json:"repositories"`
		IsTruncated bool         `json:"truncated,omitempty"`
	}
	err := keppel.ForeachRow(a.db, query, bindValues, func(rows *sql.Rows) error {
		var r Repository
		err := rows.Scan(&r.Name, &r.ManifestCount, &r.TagCount)
		if err == nil {
			result.Repos = append(result.Repos, r)
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
