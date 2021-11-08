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

//UserType is an enum that identifies the general type of user. User types are
//important because certain API endpoints or certain behavior is restricted to
//specific user types. For example, anonymous users may not cause implicit
//replications to occur, and peer users are exempt from rate limits.
type UserType int

const (
	//RegularUser is the UserType for regular users that authenticated via the AuthDriver.
	RegularUser UserType = iota
	//AnonymousUser is the UserType for unauthenticated users.
	AnonymousUser
	//PeerUser is the UserType for peer users, i.e. other Keppel instances using the API as a peer.
	PeerUser
	//JanitorUser is a dummy UserType for when the janitor needs an Authorization for audit logging purposes.
	JanitorUser
)

//UserIdentity describes the identity and access rights of a user. For regular
//users, it is returned by methods in the AuthDriver interface. For all other
//types of users, it is implicitly created in helper methods higher up in the
//stack.
type UserIdentity interface {
	//Returns whether the given auth tenant grants the given permission to this user.
	//The AnonymousUserIdentity always returns false.
	HasPermission(perm Permission, tenantID string) bool

	//Identifies the type of user that was authenticated.
	UserType() UserType
	//Returns the name of the the user that was authenticated. This should be the
	//same format that is given as the first argument of AuthenticateUser().
	//The AnonymousUserIdentity always returns the empty string.
	UserName() string
	//If this identity is backed by a Keystone token, return a UserInfo for that
	//token. Returns nil otherwise, especially for all anonymous and peer users.
	//
	//If non-nil, the Keppel API will submit OpenStack CADF audit events.
	UserInfo() audittools.UserInfo

	//SerializeToJSON serializes this UserIdentity instance into JSON for
	//inclusion in a token payload. The `typeName` must be identical to the
	//`name` argument of the RegisterUserIdentity call for this type.
	SerializeToJSON() (typeName string, payload []byte, err error)
}

var authzDeserializers = make(map[string]func([]byte, AuthDriver) (UserIdentity, error))

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
// EmbeddedAuthorization (TODO move into package auth as private type)

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
