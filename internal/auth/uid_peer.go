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
	"context"
	"encoding/json"

	"github.com/sapcc/go-bits/audittools"
	"golang.org/x/crypto/bcrypt"

	"github.com/sapcc/keppel/internal/keppel"
)

func init() {
	keppel.UserIdentityRegistry.Add(func() keppel.UserIdentity { return &PeerUserIdentity{} })
}

// PeerUserIdentity is a keppel.UserIdentity for peer users with global read
// access and access to the specialized peer API.
//
// This type used to be called ReplicationUserIdentity, which is why usernames
// start with `replication@` and why serialization uses the type name "repl".
type PeerUserIdentity struct {
	PeerHostName string
}

// UserType implements the keppel.UserIdentity interface.
func (uid *PeerUserIdentity) PluginTypeID() string {
	return "repl"
}

// HasPermission implements the keppel.UserIdentity interface.
func (uid *PeerUserIdentity) HasPermission(perm keppel.Permission, tenantID string) bool {
	//allow universal pull access for replication purposes
	return perm == keppel.CanViewAccount || perm == keppel.CanPullFromAccount
}

// UserType implements the keppel.UserIdentity interface.
func (uid *PeerUserIdentity) UserType() keppel.UserType {
	return keppel.PeerUser
}

// UserName implements the keppel.UserIdentity interface.
func (uid *PeerUserIdentity) UserName() string {
	return "replication@" + uid.PeerHostName
}

// UserInfo implements the keppel.UserIdentity interface.
func (uid *PeerUserIdentity) UserInfo() audittools.UserInfo {
	return nil
}

// SerializeToJSON implements the keppel.UserIdentity interface.
func (uid *PeerUserIdentity) SerializeToJSON() (payload []byte, err error) {
	return json.Marshal(uid.PeerHostName)
}

// DeserializeFromJSON implements the keppel.UserIdentity interface.
func (uid *PeerUserIdentity) DeserializeFromJSON(in []byte, _ keppel.AuthDriver) error {
	return json.Unmarshal(in, &uid.PeerHostName)
}

// Returns whether the given peer credentials are valid. On success, the Peer
// instance is returned. If the credentials do not match, (nil, nil) is
// returned. Error values are only returned for unexpected failures.
func checkPeerCredentials(ctx context.Context, db *keppel.DB, peerHostName, password string) (*keppel.Peer, error) {
	//NOTE: This function is technically vulnerable to a timing side-channel attack.
	//It returns much faster if `peerHostName` refers to a peer that does not exist,
	//so an attacker could use it to infer which peers exist. I don't consider
	//this an actual vulnerability since the set of peers is common knowledge:
	//In fact, it's literally exposed in an API call in the Keppel API.

	var peer keppel.Peer
	err := db.WithContext(ctx).SelectOne(&peer, `SELECT * FROM peers WHERE hostname = $1`, peerHostName)
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
