/*******************************************************************************
*
* Copyright 2020 SAP SE
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

package authapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sapcc/go-bits/respondwith"
)

func (a *API) handlePostPeering(w http.ResponseWriter, r *http.Request) {
	//decode request body
	var req struct {
		PeerHostName string `json:"peer"`
		UserName     string `json:"username"`
		Password     string `json:"password"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&req)
	if err != nil {
		http.Error(w, "request body is not valid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	//check that these credentials are intended for us
	if req.UserName != "replication@"+a.cfg.APIPublicHostname() {
		http.Error(w, "wrong audience", http.StatusBadRequest)
		return
	}

	//do we even know that guy? :)
	if !a.cfg.IsPeerHostName[req.PeerHostName] {
		http.Error(w, "unknown issuer", http.StatusBadRequest)
		return
	}

	//check that these credentials work
	authURL := fmt.Sprintf("https://%s/keppel/v1/auth", req.PeerHostName)
	authReq, err := http.NewRequest("GET", authURL, nil)
	if respondwith.ErrorText(w, err) {
		return
	}
	authHash := base64.StdEncoding.EncodeToString([]byte(
		req.UserName + ":" + req.Password,
	))
	authReq.Header.Set("Authorization", "Basic "+authHash)
	authResp, err := http.DefaultClient.Do(authReq)
	if err != nil {
		http.Error(w, "could not validate credentials: "+err.Error(), http.StatusUnauthorized)
		return
	}
	if authResp.StatusCode != http.StatusOK {
		http.Error(w, "could not validate credentials: expected 200 OK, but got "+authResp.Status, http.StatusUnauthorized)
		return
	}

	//update database
	result, err := a.db.Exec(
		`UPDATE peers SET our_password = $1 WHERE hostname = $2`,
		req.Password, req.PeerHostName,
	)
	if respondwith.ErrorText(w, err) {
		return
	}
	rowsUpdated, err := result.RowsAffected()
	if respondwith.ErrorText(w, err) {
		return
	}
	if rowsUpdated == 0 {
		//This should never occur. We checked `a.cfg.IsPeerHostName`, and all
		//entries there should have an entry in the `peers` DB table because we
		//also issued passwords in the opposite direction (or at least attempted to
		//do so).
		http.Error(w, "peer not found in database", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
