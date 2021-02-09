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

	//If this authorization is backed by a Keystone token, return a UserInfo for
	//that token. Returns nil otherwise. The AnonymousAuthorization always returns nil.
	//
	//If non-nil, the Keppel API will submit OpenStack CADF audit events.
	UserInfo() audittools.UserInfo
}

//AnonymousAuthorization is a keppel.Authorization for anonymous users.
var AnonymousAuthorization = Authorization(anonAuthorization{})

type anonAuthorization struct{}

func (anonAuthorization) UserName() string {
	return ""
}
func (anonAuthorization) HasPermission(perm Permission, tenantID string) bool {
	return false
}
func (anonAuthorization) UserInfo() audittools.UserInfo {
	return nil
}

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

//UserInfo implements the keppel.Authorization interface.
func (a ReplicationAuthorization) UserInfo() audittools.UserInfo {
	return nil
}
