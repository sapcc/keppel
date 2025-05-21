// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppelv1

import (
	"net/http"

	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

////////////////////////////////////////////////////////////////////////////////
// data types

// Peer represents a peer in the API.
type Peer struct {
	HostName string `json:"hostname"`
}

////////////////////////////////////////////////////////////////////////////////
// data conversion/validation functions

func renderPeer(p models.Peer) Peer {
	return Peer{
		HostName: p.HostName,
	}
}

func renderPeers(peers []models.Peer) []Peer {
	result := make([]Peer, len(peers))
	for idx, peer := range peers {
		result[idx] = renderPeer(peer)
	}
	return result
}

func (a *API) handleGetPeers(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/peers")
	uid, authErr := a.authDriver.AuthenticateUserFromRequest(r)
	if respondWithAuthError(w, authErr) {
		return
	}
	if uid == nil {
		respondWithAuthError(w, keppel.ErrUnauthorized.With("unauthorized"))
		return
	}

	var peers []models.Peer
	_, err := a.db.Select(&peers, `SELECT * FROM peers ORDER BY hostname`)
	if respondwith.ErrorText(w, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, map[string][]Peer{"peers": renderPeers(peers)})
}
