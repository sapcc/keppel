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

package auth

import (
	"github.com/sapcc/keppel/internal/keppel"
	"golang.org/x/crypto/bcrypt"
)

//CheckPeerCredentials returns whether the given peer credentials are valid. On
//success, the Peer instance is returned. If the credentials do not match,
//(nil, nil) is returned. Error values are only returned for unexpected
//failures.
func CheckPeerCredentials(db *keppel.DB, peerHostName, password string) (*keppel.Peer, error) {
	//NOTE: This function is technically vulnerable to a timing side-channel attack.
	//It returns much faster if `peerHostName` refers to a peer that does not exist,
	//so an attacker could use it to infer which peers exist. I don't consider
	//this an actual vulnerability since the set of peers is common knowledge:
	//In fact, it's literally exposed in an API call in the Keppel API.

	var peer keppel.Peer
	err := db.SelectOne(&peer, `SELECT * FROM peers WHERE hostname = $1`, peerHostName)
	if err != nil {
		return nil, err
	}
	hashes := []string{peer.TheirCurrentPasswordHash, peer.TheirPreviousPasswordHash}
	for _, hash := range hashes {
		if hash != "" && bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil {
			return &peer, nil
		}
	}
	return nil, nil
}
