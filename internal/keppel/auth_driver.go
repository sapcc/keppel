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
	"encoding/base64"
	"errors"
	"net/http"

	"github.com/sapcc/go-bits/gopherpolicy"
)

//Permission is an enum used by AuthDriver.
type Permission string

const (
	//CanViewAccount is the permission for viewing account metadata.
	CanViewAccount Permission = "view"
	//CanPullFromAccount is the permission for pulling images from this account.
	CanPullFromAccount Permission = "pull"
	//CanPushToAccount is the permission for pushing images to this account.
	CanPushToAccount Permission = "push"
	//CanDeleteFromAccount is the permission for deleting manifests from this account.
	CanDeleteFromAccount Permission = "delete"
	//CanChangeAccount is the permission for creating and updating accounts.
	CanChangeAccount Permission = "change"
	//CanViewQuotas is the permission for viewing an auth tenant's quotas.
	CanViewQuotas Permission = "viewquota"
	//CanChangeQuotas is the permission for changing an auth tenant's quotas.
	CanChangeQuotas Permission = "changequota"
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

	//If this authorization is backed by a Keystone token, return that token.
	//Returns nil otherwise. The AnonymousAuthorization always returns nil.
	//
	//NOTE: This breaks separation of concerns, but we need the token in the
	//Keppel API to fill audittools.EventParameters values, and I don't see a
	//more logical way to order responsibilities here that does not require a
	//"backdoor" like this method.
	KeystoneToken() *gopherpolicy.Token
}

//AuthDriver represents an authentication backend that supports multiple
//tenants. A tenant is a scope where users can be authorized to perform certain
//actions. For example, in OpenStack, a Keppel tenant is a Keystone project.
type AuthDriver interface {
	//DriverName returns the name of the auth driver as specified in
	//RegisterAuthDriver() and, therefore, the KEPPEL_AUTH_DRIVER variable.
	DriverName() string

	//ValidateTenantID checks if the given string is a valid tenant ID. If so,
	//nil shall be returned. If not, the returned error shall explain why the ID
	//is not valid. The driver implementor can decide how thorough this check
	//shall be: It can be anything from "is not empty" to "matches regex" to
	//"exists in the auth database".
	ValidateTenantID(tenantID string) error

	//SetupAccount sets up the given tenant so that it can be used for the given
	//Keppel account. The caller must supply an Authorization that was obtained
	//from one of the AuthenticateUserXXX methods of the same instance, because
	//this operation may require more permissions than Keppel itself has.
	SetupAccount(account Account, an Authorization) error

	//AuthenticateUser authenticates the user identified by the given username
	//and password. Note that usernames may not contain colons, because
	//credentials are encoded by clients in the "username:password" format.
	AuthenticateUser(userName, password string) (Authorization, *RegistryV2Error)
	//AuthenticateUserFromRequest reads credentials from the given incoming HTTP
	//request to authenticate the user which makes this request. The
	//implementation shall follow the conventions of the concrete backend, e.g. a
	//OAuth backend could try to read a Bearer token from the Authorization
	//header, whereas an OpenStack auth driver would look for a Keystone token in the
	//X-Auth-Token header.
	AuthenticateUserFromRequest(r *http.Request) (Authorization, *RegistryV2Error)
}

var authDriverFactories = make(map[string]func() (AuthDriver, error))

//NewAuthDriver creates a new AuthDriver using one of the factory functions
//registered with RegisterAuthDriver().
func NewAuthDriver(name string) (AuthDriver, error) {
	factory := authDriverFactories[name]
	if factory != nil {
		return factory()
	}
	return nil, errors.New("no such auth driver: " + name)
}

//RegisterAuthDriver registers an AuthDriver. Call this from func init() of the
//package defining the AuthDriver.
func RegisterAuthDriver(name string, factory func() (AuthDriver, error)) {
	if _, exists := authDriverFactories[name]; exists {
		panic("attempted to register multiple auth drivers with name = " + name)
	}
	authDriverFactories[name] = factory
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
func (anonAuthorization) KeystoneToken() *gopherpolicy.Token {
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

//KeystoneToken implements the keppel.Authorization interface.
func (a ReplicationAuthorization) KeystoneToken() *gopherpolicy.Token {
	return nil
}

//BuildBasicAuthHeader constructs the value of an "Authorization" HTTP header for the given basic auth credentials.
func BuildBasicAuthHeader(userName, password string) string {
	creds := userName + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(creds))
}
