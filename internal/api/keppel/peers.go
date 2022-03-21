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

package keppelv1

import (
	"net/http"

	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/go-bits/sre"

	"github.com/sapcc/keppel/internal/keppel"
)

////////////////////////////////////////////////////////////////////////////////
// data types

//Peer represents a peer in the API.
type Peer struct {
	HostName string `json:"hostname"`
}

////////////////////////////////////////////////////////////////////////////////
// data conversion/validation functions

func renderPeer(p keppel.Peer) Peer {
	return Peer{
		HostName: p.HostName,
	}
}

func renderPeers(peers []keppel.Peer) []Peer {
	result := make([]Peer, len(peers))
	for idx, peer := range peers {
		result[idx] = renderPeer(peer)
	}
	return result
}

func (a *API) handleGetPeers(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/keppel/v1/peers")
	uid, authErr := a.authDriver.AuthenticateUserFromRequest(r)
	if respondWithAuthError(w, authErr) {
		return
	}
	if uid == nil {
		respondWithAuthError(w, keppel.ErrUnauthorized.With("unauthorized"))
		return
	}

	var peers []keppel.Peer
	_, err := a.db.Select(&peers, `SELECT * FROM peers ORDER BY hostname`)
	if respondwith.ErrorText(w, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, map[string][]Peer{"peers": renderPeers(peers)})
}
