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

//UserIdentity describes the identity and access rights of a user. For regular
//users, it is returned by methods in the AuthDriver interface. Janitor tasks
//may spawn UserIdentity instances solely for the purpose of audit logging.
//
//TODO do not use for anonymous user
//TODO do not use for replication user
type UserIdentity interface {
	//Returns whether the given auth tenant grants the given permission to this user.
	//The AnonymousUserIdentity always returns false.
	HasPermission(perm Permission, tenantID string) bool

	//IsRegularUser indicates if this token is for a regular user, not for an
	//anonymous user or an internal service user.
	//
	//TODO remove
	IsRegularUser() bool
	//IsReplicationUser indicates if this token is for an internal service user
	//used only for replication. (Some special rules apply for those service
	//users, e.g. rate limit exemptions.)
	//
	//TODO remove
	IsReplicationUser() bool

	//SerializeToJSON serializes this UserIdentity instance into JSON for
	//inclusion in a token payload. The `typeName` must be identical to the
	//`name` argument of the RegisterUserIdentity call for this type.
	SerializeToJSON() (typeName string, payload []byte, err error)

	//Returns the name of the the user that was authenticated. This should be the
	//same format that is given as the first argument of AuthenticateUser().
	//The AnonymousUserIdentity always returns the empty string.
	UserName() string
	//If this authorization is backed by a Keystone token, return a UserInfo for
	//that token. Returns nil otherwise. The AnonymousUserIdentity always returns nil.
	//
	//If non-nil, the Keppel API will submit OpenStack CADF audit events.
	UserInfo() audittools.UserInfo
}

var authzDeserializers = map[string]func([]byte, AuthDriver) (UserIdentity, error){
	"anon":    deserializeAnonUserIdentity,
	"janitor": deserializeJanitorUserIdentity,
	"repl":    deserializeReplUserIdentity,
}

//RegisterUserIdentity registers a type implementing the UserIdentity
//interface. Call this from func init() of the package defining the type.
//
//The `deserialize` function is called whenever an instance of this type needs to
//be deserialized from a token payload. It shall perform the exact reverse of
//the type's SerializeToJSON method.
func RegisterUserIdentity(name string, deserialize func([]byte, AuthDriver) (UserIdentity, error)) {
	if _, exists := authzDeserializers[name]; exists {
		panic("attempted to register multiple UserIdentity types with name = " + name)
	}
	authzDeserializers[name] = deserialize
}

type compressedPayload struct {
	Contents []byte `json:"gzip"`
}

//CompressTokenPayload can be used by types implementing the UserIdentity
//interface to compress large token payloads with GZip or similar. (The exact
//compression format is an implementation detail.) The result is a valid JSON
//message that self-documents the compression algorithm that was used.
func CompressTokenPayload(payload []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	_, err := writer.Write(payload)
	if err == nil {
		err = writer.Close()
	}
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
// AnonymousUserIdentity

//AnonymousUserIdentity is a keppel.UserIdentity for anonymous users.
var AnonymousUserIdentity = UserIdentity(anonUserIdentity{})

type anonUserIdentity struct{}

func (anonUserIdentity) UserName() string {
	return ""
}
func (anonUserIdentity) HasPermission(perm Permission, tenantID string) bool {
	return false
}
func (anonUserIdentity) IsRegularUser() bool {
	return false
}
func (anonUserIdentity) IsReplicationUser() bool {
	return false
}
func (anonUserIdentity) SerializeToJSON() (typeName string, payload []byte, err error) {
	return "anon", []byte("true"), nil
}
func (anonUserIdentity) UserInfo() audittools.UserInfo {
	return nil
}

func deserializeAnonUserIdentity(in []byte, _ AuthDriver) (UserIdentity, error) {
	if string(in) != "true" {
		return nil, fmt.Errorf("%q is not a valid payload for AnonymousUserIdentity", string(in))
	}
	return AnonymousUserIdentity, nil
}

////////////////////////////////////////////////////////////////////////////////
// JanitorUserIdentity

//JanitorUserIdentity is a keppel.UserIdentity for the janitor user. (It's
//only used for generating audit events.)
type JanitorUserIdentity struct {
	TaskName string
	GCPolicy *GCPolicy
}

//UserName implements the keppel.UserIdentity interface.
func (JanitorUserIdentity) UserName() string {
	return ""
}

//HasPermission implements the keppel.UserIdentity interface.
func (JanitorUserIdentity) HasPermission(perm Permission, tenantID string) bool {
	return false
}

//IsRegularUser implements the keppel.UserIdentity interface.
func (JanitorUserIdentity) IsRegularUser() bool {
	return false
}

//IsReplicationUser implements the keppel.UserIdentity interface.
func (JanitorUserIdentity) IsReplicationUser() bool {
	return false
}

//SerializeToJSON implements the keppel.UserIdentity interface.
func (a JanitorUserIdentity) SerializeToJSON() (typeName string, payload []byte, err error) {
	serialized := []byte(a.TaskName)
	if a.GCPolicy != nil {
		policyJSON, err := json.Marshal(*a.GCPolicy)
		if err != nil {
			return "", nil, err
		}
		serialized = append(append(serialized, ':'), policyJSON...)
	}
	return "janitor", serialized, nil
}

//UserInfo implements the keppel.UserIdentity interface.
func (a JanitorUserIdentity) UserInfo() audittools.UserInfo {
	return janitorUserInfo(a)
}

func deserializeJanitorUserIdentity(in []byte, _ AuthDriver) (UserIdentity, error) {
	//simple case: no GCPolicy, just a TaskName
	fields := bytes.SplitN(in, []byte(":"), 2)
	if len(fields) == 1 {
		return JanitorUserIdentity{string(in), nil}, nil
	}

	//with GCPolicy: TaskName and GCPolicyJSON separated by colon
	var gcPolicy GCPolicy
	err := json.Unmarshal(fields[1], &gcPolicy)
	return JanitorUserIdentity{string(fields[0]), &gcPolicy}, err
}

////////////////////////////////////////////////////////////////////////////////
// ReplicationUserIdentity

//ReplicationUserIdentity is a keppel.UserIdentity for replication users with global pull access.
type ReplicationUserIdentity struct {
	PeerHostName string
}

//UserName implements the keppel.UserIdentity interface.
func (a ReplicationUserIdentity) UserName() string {
	return "replication@" + a.PeerHostName
}

//HasPermission implements the keppel.UserIdentity interface.
func (a ReplicationUserIdentity) HasPermission(perm Permission, tenantID string) bool {
	return perm == CanViewAccount || perm == CanPullFromAccount
}

//IsRegularUser implements the keppel.UserIdentity interface.
func (a ReplicationUserIdentity) IsRegularUser() bool {
	return false
}

//IsReplicationUser implements the keppel.UserIdentity interface.
func (a ReplicationUserIdentity) IsReplicationUser() bool {
	return true
}

//SerializeToJSON implements the keppel.UserIdentity interface.
func (a ReplicationUserIdentity) SerializeToJSON() (typeName string, payload []byte, err error) {
	payload, err = json.Marshal(a.PeerHostName)
	return "repl", payload, err
}

//UserInfo implements the keppel.UserIdentity interface.
func (a ReplicationUserIdentity) UserInfo() audittools.UserInfo {
	return nil
}

func deserializeReplUserIdentity(in []byte, _ AuthDriver) (UserIdentity, error) {
	var peerHostName string
	err := json.Unmarshal(in, &peerHostName)
	return ReplicationUserIdentity{peerHostName}, err
}

////////////////////////////////////////////////////////////////////////////////
// EmbeddedAuthorization

//EmbeddedAuthorization wraps an UserIdentity such that it can be serialized into JSON.
type EmbeddedAuthorization struct {
	UserIdentity UserIdentity
	//AuthDriver is ignored during serialization, but must be filled prior to
	//deserialization because some types of UserIdentity require their
	//respective AuthDriver to deserialize properly.
	AuthDriver AuthDriver
}

//MarshalJSON implements the json.Marshaler interface.
func (ea EmbeddedAuthorization) MarshalJSON() ([]byte, error) {
	typeName, payload, err := ea.UserIdentity.SerializeToJSON()
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
		ea.UserIdentity, err = deserializer([]byte(payload), ea.AuthDriver)
		return err
	}

	//the loop body executes exactly once, therefore this location is unreachable
	panic("unreachable")
}
