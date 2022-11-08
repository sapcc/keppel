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
