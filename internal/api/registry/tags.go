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
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	distspecv1 "github.com/opencontainers/distribution-spec/specs-go/v1"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/go-bits/sqlext"
)

var tagsListQuery = sqlext.SimplifyWhitespace(`
	SELECT name FROM tags
	 WHERE repo_id = $1 AND (name > $2 or $2 = '')
	 ORDER BY name ASC LIMIT $3
`)

func (a *API) handleListTags(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/v2/:account/:repo/tags/list")
	account, repo, _, _ := a.checkAccountAccess(w, r, failIfRepoMissing, a.handleListTagsAnycast)
	if account == nil {
		return
	}

	// parse query: limit (parameter "n")
	query := r.URL.Query()
	var (
		limit uint64
		err   error
	)
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

	// parse query: marker (parameter "last")
	marker := query.Get("last")

	// list tags (we request one more than `limit` to see if we need to paginate)
	tags := []string{}
	err = sqlext.ForeachRow(a.db, tagsListQuery, []any{repo.ID, marker, limit + 1}, func(rows *sql.Rows) error {
		var tagName string
		err = rows.Scan(&tagName)
		if err == nil {
			tags = append(tags, tagName)
		}
		return err
	})
	if respondWithError(w, r, err) {
		return
	}

	// do we need to paginate?
	if uint64(len(tags)) > limit {
		tags = tags[0:limit]
		linkQuery := url.Values{}
		linkQuery.Set("n", strconv.FormatUint(limit, 10))
		linkQuery.Set("last", tags[len(tags)-1])
		linkURL := url.URL{
			Path:     fmt.Sprintf("/v2/%s/tags/list", repo.FullName()),
			RawQuery: linkQuery.Encode(),
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, linkURL.String()))
	}

	respondwith.JSON(w, http.StatusOK, distspecv1.TagList{
		Name: repo.FullName(),
		Tags: tags,
	})
}

func (a *API) handleListTagsAnycast(w http.ResponseWriter, r *http.Request, info anycastRequestInfo) {
	err := a.cfg.ReverseProxyAnycastRequestToPeer(w, r, info.PrimaryHostName)
	if respondWithError(w, r, err) {
		return
	}
}
