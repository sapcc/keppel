/*******************************************************************************
*
* Copyright 2024 SAP SE
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
