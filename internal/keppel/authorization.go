/*******************************************************************************
*
* Copyright 2018 SAP SE
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
	"encoding/json"
	"fmt"

	"github.com/sapcc/go-bits/audittools"
)

//Authorization describes the access rights for a user. It is returned by
//methods in the AuthDriver interface.
type Authorization interface {
	//Returns the name of the the user that was authenticated. This should be the
	//same format that is given as the first argument of AuthenticateUser().
	//The AnonymousAuthorization always returns the empty string.
	UserName() string
	//Returns whether the given auth tenant grants the given permission to this user.
	//The AnonymousAuthorization always returns false.
	HasPermission(perm Permission, tenantID string) bool

	//SerializeToJSON serializes this Authorization instance into JSON for
	//inclusion in a token payload. The `typeName` must be identical to the
	//`name` argument of the RegisterAuthorization call for this type.
	SerializeToJSON() (typeName string, payload []byte, err error)

	//If this authorization is backed by a Keystone token, return a UserInfo for
	//that token. Returns nil otherwise. The AnonymousAuthorization always returns nil.
	//
	//If non-nil, the Keppel API will submit OpenStack CADF audit events.
	UserInfo() audittools.UserInfo
}

var authzDeserializers = map[string]func([]byte, AuthDriver) (Authorization, error){
	"anon": deserializeAnonAuthorization,
	"repl": deserializeReplAuthorization,
}

//RegisterAuthorization registers a type implementing the Authorization
//interface. Call this from func init() of the package defining the type.
//
//The `deserialize` function is called whenever an instance of this type needs to
//be deserialized from a token payload. It shall perform the exact reverse of
//the type's SerializeToJSON method.
func RegisterAuthorization(name string, deserialize func([]byte, AuthDriver) (Authorization, error)) {
	if _, exists := authzDeserializers[name]; exists {
		panic("attempted to register multiple Authorization types with name = " + name)
	}
	authzDeserializers[name] = deserialize
}

////////////////////////////////////////////////////////////////////////////////
// AnonymousAuthorization

//AnonymousAuthorization is a keppel.Authorization for anonymous users.
var AnonymousAuthorization = Authorization(anonAuthorization{})

type anonAuthorization struct{}

func (anonAuthorization) UserName() string {
	return ""
}
func (anonAuthorization) HasPermission(perm Permission, tenantID string) bool {
	return false
}
func (anonAuthorization) SerializeToJSON() (typeName string, payload []byte, err error) {
	return "anon", []byte("true"), nil
}
func (anonAuthorization) UserInfo() audittools.UserInfo {
	return nil
}

func deserializeAnonAuthorization(in []byte, _ AuthDriver) (Authorization, error) {
	if string(in) != "true" {
		return nil, fmt.Errorf("%q is not a valid payload for AnonymousAuthorization", string(in))
	}
	return AnonymousAuthorization, nil
}

////////////////////////////////////////////////////////////////////////////////
// ReplicationAuthorization

//ReplicationAuthorization is a keppel.Authorization for replication users with global pull access.
type ReplicationAuthorization struct {
	PeerHostName string
}

//UserName implements the keppel.Authorization interface.
func (a ReplicationAuthorization) UserName() string {
	return "replication@" + a.PeerHostName
}

//HasPermission implements the keppel.Authorization interface.
func (a ReplicationAuthorization) HasPermission(perm Permission, tenantID string) bool {
	return perm == CanViewAccount || perm == CanPullFromAccount
}

//SerializeToJSON implements the keppel.Authorization interface.
func (a ReplicationAuthorization) SerializeToJSON() (typeName string, payload []byte, err error) {
	payload, err = json.Marshal(a.PeerHostName)
	return "repl", payload, err
}

//UserInfo implements the keppel.Authorization interface.
func (a ReplicationAuthorization) UserInfo() audittools.UserInfo {
	return nil
}

func deserializeReplAuthorization(in []byte, _ AuthDriver) (Authorization, error) {
	var peerHostName string
	err := json.Unmarshal(in, &peerHostName)
	return ReplicationAuthorization{peerHostName}, err
}
