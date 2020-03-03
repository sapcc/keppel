/******************************************************************************
*
*  Copyright 2020 SAP SE
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
	"net/http"
	"strconv"

	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/go-bits/sre"
	"github.com/sapcc/keppel/internal/keppel"
)

const tagsListQuery = `
	SELECT name FROM tags
	 WHERE repo_id = $1 AND (name > $2 or $2 = '')
	 ORDER BY name ASC LIMIT $3
`

func (a *API) handleListTags(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/v2/:account/:repo/tags/list")
	account, repoName, _ := a.checkAccountAccess(w, r)
	if account == nil {
		return
	}

	repo, err := keppel.FindRepository(a.db, repoName, *account)
	if err == sql.ErrNoRows {
		keppel.ErrNameUnknown.With("no such repository").WriteAsRegistryV2ResponseTo(w)
		return
	}
	if respondWithError(w, err) {
		return
	}

	//parse query: limit (parameter "n")
	query := r.URL.Query()
	var limit uint64
	if limitStr := query.Get("n"); limitStr != "" {
		limit, err = strconv.ParseUint(limitStr, 10, 64)
		if err != nil {
			http.Error(w, `invalid value for "n": `+err.Error(), http.StatusBadRequest)
			return
		}
		if limit == 0 {
			http.Error(w, `invalid value for "n": must not be 0`, http.StatusBadRequest)
			return
		}
	} else {
		limit = maxLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	//parse query: marker (parameter "last")
	marker := query.Get("last")

	//list tags
	var tags []string
	err = keppel.ForeachRow(a.db, tagsListQuery, []interface{}{repo.ID, marker, limit}, func(rows *sql.Rows) error {
		var tagName string
		err = rows.Scan(&tagName)
		if err == nil {
			tags = append(tags, tagName)
		}
		return err
	})
	if respondWithError(w, err) {
		return
	}

	respondwith.JSON(w, http.StatusOK,
		struct {
			RepoName string   `json:"name"`
			Tags     []string `json:"tags"`
		}{
			RepoName: repo.FullName(),
			Tags:     tags,
		},
	)
}
