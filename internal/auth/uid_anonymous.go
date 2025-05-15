// SPDX-FileCopyrightText: 2021 SAP SE
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"

	"github.com/sapcc/go-bits/audittools"

	"github.com/sapcc/keppel/internal/keppel"
)

func init() {
	keppel.UserIdentityRegistry.Add(func() keppel.UserIdentity { return AnonymousUserIdentity })
}

// AnonymousUserIdentity is a keppel.UserIdentity for anonymous users.
var AnonymousUserIdentity = keppel.UserIdentity(anonUserIdentity{})

type anonUserIdentity struct{}

// PluginTypeID implements the keppel.UserIdentity interface.
func (anonUserIdentity) PluginTypeID() string {
	return "anon"
}

// HasPermission implements the keppel.UserIdentity interface.
func (anonUserIdentity) HasPermission(perm keppel.Permission, tenantID string) bool {
	return false
}

// UserType implements the keppel.UserIdentity interface.
func (anonUserIdentity) UserType() keppel.UserType {
	return keppel.AnonymousUser
}

// UserName implements the keppel.UserIdentity interface.
func (anonUserIdentity) UserName() string {
	return ""
}

// UserInfo implements the keppel.UserIdentity interface.
func (anonUserIdentity) UserInfo() audittools.UserInfo {
	return nil
}

// SerializeToJSON implements the keppel.UserIdentity interface.
func (anonUserIdentity) SerializeToJSON() (payload []byte, err error) {
	return []byte("true"), nil
}

// DeserializeFromJSON implements the keppel.UserIdentity interface.
func (anonUserIdentity) DeserializeFromJSON(in []byte, _ keppel.AuthDriver) error {
	if string(in) != "true" {
		return fmt.Errorf("%q is not a valid payload for AnonymousUserIdentity", string(in))
	}
	return nil
}
