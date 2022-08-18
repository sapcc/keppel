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
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"

	"github.com/sapcc/keppel/internal/keppel"
)

// PeeringRequest is the structure of the JSON request body sent to the POST
// /keppel/v1/auth/peering endpoint.
type PeeringRequest struct {
	PeerHostName string `json:"peer"`
	UserName     string `json:"username"`
	Password     string `json:"password"`
}

func (a *API) handlePostPeering(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/auth/peering")
	//decode request body
	var req PeeringRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&req)
	if err != nil {
		http.Error(w, "request body is not valid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	//check that these credentials are intended for us
	if req.UserName != "replication@"+a.cfg.APIPublicHostname {
		http.Error(w, "wrong audience", http.StatusBadRequest)
		return
	}

	//do we even know that guy? :)
	var peer keppel.Peer
	err = a.db.SelectOne(&peer, `SELECT * FROM peers WHERE hostname = $1`, req.PeerHostName)
	if err == sql.ErrNoRows {
		http.Error(w, "unknown issuer", http.StatusBadRequest)
		return
	}
	if respondwith.ErrorText(w, err) {
		return
	}

	//check that these credentials work
	authURL := fmt.Sprintf("https://%s/keppel/v1/auth?service=%[1]s", req.PeerHostName)
	authReq, err := http.NewRequest(http.MethodGet, authURL, http.NoBody)
	if respondwith.ErrorText(w, err) {
		return
	}
	authReq.Header.Set("Authorization", keppel.BuildBasicAuthHeader(req.UserName, req.Password))

	authResp, err := http.DefaultClient.Do(authReq)
	if err != nil {
		http.Error(w, "could not validate credentials: "+err.Error(), http.StatusUnauthorized)
		return
	}
	defer authResp.Body.Close()
	if authResp.StatusCode != http.StatusOK {
		http.Error(w, "could not validate credentials: expected 200 OK, but got "+authResp.Status, http.StatusUnauthorized)
		return
	}

	//update database
	_, err = a.db.Exec(
		`UPDATE peers SET our_password = $1 WHERE hostname = $2`,
		req.Password, req.PeerHostName,
	)
	if respondwith.ErrorText(w, err) {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
