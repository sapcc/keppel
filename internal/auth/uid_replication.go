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
	"encoding/json"

	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/keppel/internal/keppel"
)

func init() {
	keppel.RegisterUserIdentity("repl", deserializeReplicationUserIdentity)
}

//ReplicationUserIdentity is a keppel.UserIdentity for replication users with global pull access.
//
//TODO generalize into PeerUserIdentity
type ReplicationUserIdentity struct {
	PeerHostName string
}

//HasPermission implements the keppel.UserIdentity interface.
func (uid ReplicationUserIdentity) HasPermission(perm keppel.Permission, tenantID string) bool {
	//allow universal pull access for replication purposes
	return perm == keppel.CanViewAccount || perm == keppel.CanPullFromAccount
}

//UserType implements the keppel.UserIdentity interface.
func (uid ReplicationUserIdentity) UserType() keppel.UserType {
	return keppel.PeerUser
}

//UserName implements the keppel.UserIdentity interface.
func (uid ReplicationUserIdentity) UserName() string {
	return "replication@" + uid.PeerHostName
}

//UserInfo implements the keppel.UserIdentity interface.
func (uid ReplicationUserIdentity) UserInfo() audittools.UserInfo {
	return nil
}

//SerializeToJSON implements the keppel.UserIdentity interface.
func (uid ReplicationUserIdentity) SerializeToJSON() (typeName string, payload []byte, err error) {
	payload, err = json.Marshal(uid.PeerHostName)
	return "repl", payload, err
}

func deserializeReplicationUserIdentity(in []byte, _ keppel.AuthDriver) (keppel.UserIdentity, error) {
	var peerHostName string
	err := json.Unmarshal(in, &peerHostName)
	return ReplicationUserIdentity{peerHostName}, err
}
