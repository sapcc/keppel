// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"github.com/go-gorp/gorp/v3"

	"github.com/sapcc/keppel/internal/models"
)

// GetPeerFromAccount returns the peer of the account given.
//
// Returns sql.ErrNoRows if the configured peer does not exist.
func GetPeerFromAccount(db gorp.SqlExecutor, account models.Account) (models.Peer, error) {
	var peer models.Peer
	err := db.SelectOne(&peer, `SELECT * FROM peers WHERE hostname = $1`, account.UpstreamPeerHostName)
	if err != nil {
		return models.Peer{}, err
	}
	return peer, nil
}
