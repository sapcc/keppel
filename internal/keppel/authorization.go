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
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"

	"github.com/sapcc/go-bits/audittools"
)

//Authorization describes the access rights for a user. It is returned by
//methods in the AuthDriver interface.
type Authorization interface {
	//Returns whether the given auth tenant grants the given permission to this user.
	//The AnonymousAuthorization always returns false.
	HasPermission(perm Permission, tenantID string) bool

	//IsRegularUser indicates if this token is for a regular user, not for an
	//anonymous user or an internal service user.
	IsRegularUser() bool
	//IsReplicationUser indicates if this token is for an internal service user
	//used only for replication. (Some special rules apply for those service
	//users, e.g. rate limit exemptions.)
	IsReplicationUser() bool

	//SerializeToJSON serializes this Authorization instance into JSON for
	//inclusion in a token payload. The `typeName` must be identical to the
	//`name` argument of the RegisterAuthorization call for this type.
	SerializeToJSON() (typeName string, payload []byte, err error)

	//Returns the name of the the user that was authenticated. This should be the
	//same format that is given as the first argument of AuthenticateUser().
	//The AnonymousAuthorization always returns the empty string.
	UserName() string
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

type compressedPayload struct {
	Contents []byte `json:"gzip"`
}

//CompressTokenPayload can be used by types implementing the Authorization
//interface to compress large token payloads with GZip or similar. (The exact
//compression format is an implementation detail.) The result is a valid JSON
//message that self-documents the compression algorithm that was used.
func CompressTokenPayload(payload []byte) ([]byte, error) {
	var buf bytes.Buffer
	_, err := gzip.NewWriter(&buf).Write(payload)
	if err != nil {
		return nil, fmt.Errorf("cannot apply GZip compression: %w", err)
	}
	return json.Marshal(compressedPayload{buf.Bytes()})
}

//DecompressTokenPayload is the exact reverse of CompressTokenPayload.
func DecompressTokenPayload(payload []byte) ([]byte, error) {
	var data compressedPayload
	err := json.Unmarshal(payload, &data)
	if err != nil {
		return nil, err
	}
	var result []byte
	reader, err := gzip.NewReader(bytes.NewReader(data.Contents))
	if err == nil {
		result, err = ioutil.ReadAll(reader)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read GZip payload: %w", err)
	}
	return result, nil
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
func (anonAuthorization) IsRegularUser() bool {
	return false
}
func (anonAuthorization) IsReplicationUser() bool {
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

//IsRegularUser implements the keppel.Authorization interface.
func (a ReplicationAuthorization) IsRegularUser() bool {
	return false
}

//IsReplicationUser implements the keppel.Authorization interface.
func (a ReplicationAuthorization) IsReplicationUser() bool {
	return true
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

////////////////////////////////////////////////////////////////////////////////
// EmbeddedAuthorization

//EmbeddedAuthorization wraps an Authorization such that it can be serialized into JSON.
type EmbeddedAuthorization struct {
	Authorization Authorization
	//AuthDriver is ignored during serialization, but must be filled prior to
	//deserialization because some types of Authorization require their
	//respective AuthDriver to deserialize properly.
	AuthDriver AuthDriver
}

//MarshalJSON implements the json.Marshaler interface.
func (ea EmbeddedAuthorization) MarshalJSON() ([]byte, error) {
	typeName, payload, err := ea.Authorization.SerializeToJSON()
	if err != nil {
		return nil, err
	}

	//The straight-forward approach would be to serialize as
	//`{"type":"foo","payload":"something"}`, but we serialize as
	//`{"foo":"something"}` instead to shave off a few bytes.
	return json.Marshal(map[string]json.RawMessage{typeName: json.RawMessage(payload)})
}

//UnmarshalJSON implements the json.Marshaler interface.
func (ea *EmbeddedAuthorization) UnmarshalJSON(in []byte) error {
	if ea.AuthDriver == nil {
		return errors.New("cannot unmarshal EmbeddedAuthorization without an AuthDriver")
	}

	m := make(map[string]json.RawMessage)
	err := json.Unmarshal(in, &m)
	if err != nil {
		return err
	}
	if len(m) != 1 {
		return fmt.Errorf("cannot unmarshal EmbeddedAuthorization with %d components", len(m))
	}

	for typeName, payload := range m {
		deserializer := authzDeserializers[typeName]
		if deserializer == nil {
			return fmt.Errorf("cannot unmarshal EmbeddedAuthorization with unknown payload type %q", typeName)
		}
		ea.Authorization, err = deserializer([]byte(payload), ea.AuthDriver)
		return err
	}

	//the loop body executes exactly once, therefore this location is unreachable
	panic("unreachable")
}
