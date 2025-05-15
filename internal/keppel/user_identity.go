// SPDX-FileCopyrightText: 2018 SAP SE
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"fmt"

	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/pluggable"
)

// UserType is an enum that identifies the general type of user. User types are
// important because certain API endpoints or certain behavior is restricted to
// specific user types. For example, anonymous users may not cause implicit
// replications to occur, and peer users are exempt from rate limits.
type UserType int

const (
	// RegularUser is the UserType for regular users that authenticated via the AuthDriver.
	RegularUser UserType = iota
	// AnonymousUser is the UserType for unauthenticated users.
	AnonymousUser
	// PeerUser is the UserType for peer users, i.e. other Keppel instances using the API as a peer.
	PeerUser
	// TrivyUser is the UserType for tokens issued to Trivy.
	TrivyUser
	// JanitorUser is a dummy UserType for when the janitor needs an Authorization for audit logging purposes.
	JanitorUser
)

// UserIdentity describes the identity and access rights of a user. For regular
// users, it is returned by methods in the AuthDriver interface. For all other
// types of users, it is implicitly created in helper methods higher up in the
// stack.
type UserIdentity interface {
	pluggable.Plugin

	// Returns whether the given auth tenant grants the given permission to this user.
	// The AnonymousUserIdentity always returns false.
	HasPermission(perm Permission, tenantID string) bool

	// Identifies the type of user that was authenticated.
	UserType() UserType
	// Returns the name of the user that was authenticated. This should be the
	// same format that is given as the first argument of AuthenticateUser().
	// The AnonymousUserIdentity always returns the empty string.
	UserName() string
	// If this identity is backed by a Keystone token, return a UserInfo for that
	// token. Returns nil otherwise, especially for all anonymous and peer users.
	//
	// If non-nil, the Keppel API will submit OpenStack CADF audit events.
	UserInfo() audittools.UserInfo

	// SerializeToJSON serializes this UserIdentity instance into JSON for
	// inclusion in a token payload.
	SerializeToJSON() (payload []byte, err error)
	// DeserializeFromJSON deserializes the given token payload (as returned by
	// SerializeToJSON) into the callee. This is always called on a fresh
	// instance created by UserIdentityFactory.Instantiate().
	DeserializeFromJSON(payload []byte, ad AuthDriver) error
}

// UserIdentityRegistry is a pluggable.Registry for UserIdentity implementations.
var UserIdentityRegistry pluggable.Registry[UserIdentity]

// DeserializeUserIdentity deserializes a UserIdentity payload. This is the
// reverse of UserIdentity.SerializeToJSON().
func DeserializeUserIdentity(typeID string, payload []byte, ad AuthDriver) (UserIdentity, error) {
	uid := UserIdentityRegistry.Instantiate(typeID)
	if uid == nil {
		return nil, fmt.Errorf("cannot unmarshal embedded authorization with unknown payload type %q", typeID)
	}
	err := uid.DeserializeFromJSON(payload, ad)
	return uid, err
}
