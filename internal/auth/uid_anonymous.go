// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"encoding/json/jsontext"
	"encoding/json/v2"

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
func (anonUserIdentity) SerializeToJSON(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.True)
}

// DeserializeFromJSON implements the keppel.UserIdentity interface.
func (anonUserIdentity) DeserializeFromJSON(dec *jsontext.Decoder, _ keppel.AuthDriver) error {
	// accept only the payload `true` (exactly as emitted above)
	k := dec.PeekKind()
	if k != jsontext.KindTrue {
		return &json.SemanticError{JSONKind: k}
	}
	_, err := dec.ReadToken()
	return err
}
