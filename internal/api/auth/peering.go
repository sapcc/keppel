// SPDX-FileCopyrightText: 2020 SAP SE
// SPDX-License-Identifier: Apache-2.0

package authapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
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
	// decode request body
	var req PeeringRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&req)
	if err != nil {
		http.Error(w, "request body is not valid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// check that these credentials are intended for us
	if req.UserName != "replication@"+a.cfg.APIPublicHostname {
		http.Error(w, "wrong audience", http.StatusBadRequest)
		return
	}

	// do we even know that guy? :)
	var peer models.Peer
	err = a.db.SelectOne(&peer, `SELECT * FROM peers WHERE hostname = $1`, req.PeerHostName)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "unknown issuer", http.StatusBadRequest)
		return
	}
	if respondwith.ErrorText(w, err) {
		return
	}

	// check that these credentials work
	authURL := fmt.Sprintf("https://%s/keppel/v1/auth?service=%[1]s", req.PeerHostName)
	authReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, authURL, http.NoBody)
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

	// update database
	_, err = a.db.Exec(
		`UPDATE peers SET our_password = $1 WHERE hostname = $2`,
		req.Password, req.PeerHostName,
	)
	if respondwith.ErrorText(w, err) {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
