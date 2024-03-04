/*******************************************************************************
*
* Copyright 2021 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package peerv1

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/keppel"
)

// Implementation for the POST /peer/v1/sync-replica/:account/:repo endpoint.
func (a *API) handleSyncReplica(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/peer/v1/sync-replica/:account/:repo")
	peer := a.authenticateRequest(w, r)
	if peer == nil {
		return
	}

	//decode request body
	var req keppel.ReplicaSyncPayload
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&req)
	if err != nil {
		http.Error(w, "request body is not valid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	//find account
	account, err := keppel.FindAccount(a.db, mux.Vars(r)["account"])
	if respondwith.ErrorText(w, err) {
		return
	}
	if account == nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}

	//find repository
	repo, err := keppel.FindRepository(a.db, mux.Vars(r)["repo"], *account)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	if respondwith.ErrorText(w, err) {
		return
	}

	//if we don't have any last_pulled_at values in the request, we can skip
	//preparing the respective UPDATE statements below
	hasManifestsLastPulledAt := false
	hasTagsLastPulledAt := false
	for _, m := range req.Manifests {
		if m.LastPulledAt != nil {
			hasManifestsLastPulledAt = true
		}
		for _, t := range m.Tags {
			if t.LastPulledAt != nil {
				hasTagsLastPulledAt = true
			}
		}
	}

	//update our own last_pulled_at timestamps to cover pulls performed on the replica side
	query := `UPDATE manifests SET last_pulled_at = $3 WHERE repo_id = $1 AND digest = $2 AND (last_pulled_at IS NULL OR last_pulled_at < $3)`
	if hasManifestsLastPulledAt {
		err = sqlext.WithPreparedStatement(a.db, query, func(stmt *sql.Stmt) error {
			for _, m := range req.Manifests {
				if m.LastPulledAt == nil {
					continue
				}
				_, err := stmt.Exec(repo.ID, m.Digest, time.Unix(*m.LastPulledAt, 0))
				if err != nil {
					return err
				}
			}
			return nil
		})
		if respondwith.ErrorText(w, err) {
			return
		}
	}

	query = `UPDATE tags SET last_pulled_at = $4 WHERE repo_id = $1 AND digest = $2 AND name = $3 AND (last_pulled_at IS NULL OR last_pulled_at < $4)`
	if hasTagsLastPulledAt {
		err = sqlext.WithPreparedStatement(a.db, query, func(stmt *sql.Stmt) error {
			for _, m := range req.Manifests {
				for _, t := range m.Tags {
					if t.LastPulledAt == nil {
						continue
					}
					_, err := stmt.Exec(repo.ID, m.Digest, t.Name, time.Unix(*t.LastPulledAt, 0))
					if err != nil {
						return err
					}
				}
			}
			return nil
		})
		if respondwith.ErrorText(w, err) {
			return
		}
	}

	//gather the data for our side of the bargain
	tagsByDigest := make(map[digest.Digest][]keppel.TagForSync)
	query = `SELECT name, digest FROM tags WHERE repo_id = $1`
	err = sqlext.ForeachRow(a.db, query, []any{repo.ID}, func(rows *sql.Rows) error {
		var (
			name   string
			digest digest.Digest
		)
		err = rows.Scan(&name, &digest)
		if err != nil {
			return err
		}
		tagsByDigest[digest] = append(tagsByDigest[digest], keppel.TagForSync{Name: name})
		return nil
	})
	if respondwith.ErrorText(w, err) {
		return
	}

	var manifests []keppel.ManifestForSync
	query = `SELECT digest FROM manifests WHERE repo_id = $1`
	err = sqlext.ForeachRow(a.db, query, []any{repo.ID}, func(rows *sql.Rows) error {
		var digest digest.Digest
		err = rows.Scan(&digest)
		if err != nil {
			return err
		}
		manifests = append(manifests, keppel.ManifestForSync{
			Digest: digest,
			Tags:   tagsByDigest[digest],
		})
		return nil
	})
	if respondwith.ErrorText(w, err) {
		return
	}

	respondwith.JSON(w, http.StatusOK, keppel.ReplicaSyncPayload{Manifests: manifests})
}
