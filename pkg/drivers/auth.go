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

package drivers

import (
	"errors"
	"net/http"

	"github.com/sapcc/keppel/pkg/database"
)

//Permission is an enum used by AuthDriver.
type Permission int

const (
	_nullPermission Permission = iota
	//CanViewAccount is the permission for viewing account metadata.
	CanViewAccount
	//CanPullFromAccount is the permission for pulling images from this account.
	CanPullFromAccount
	//CanPushToAccount is the permission for pushing images to this account.
	CanPushToAccount
	//CanChangeAccount is the permission for creating and updating accounts.
	CanChangeAccount
)

//Authorization describes the access rights for some user, possibly limited to
//some tenant. It is returned by methods in the AuthDriver interface.
type Authorization interface {
	HasPermission(perm Permission, tenantID string) bool
}

//AuthDriver represents an authentication backend that supports multiple
//tenants. A tenant is a scope where users can be authorized to perform certain
//actions. For example, in OpenStack, a Keppel tenant is a Keystone project.
type AuthDriver interface {
	//ReadConfig unmarshals the configuration for this driver type into this
	//driver instance. The `unmarshal` function works exactly like in
	//UnmarshalYAML. This method shall only fail if the input data is malformed.
	//It shall not make any network requests; use Connect for that.
	ReadConfig(unmarshal func(interface{}) error) error
	//Connect prepares this driver instance for usage. This is called *after*
	//ReadConfig and *before* any other methods are called.
	Connect() error

	//SetupAccount sets up the given tenant so that it can be used for the given
	//Keppel account. The caller must supply an Authorization that was obtained
	//from one of the AuthenticateUserXXX methods of the same instance, because
	//this operation may require more permissions than Keppel itself has.
	SetupAccount(account database.Account, an Authorization) error
	//AuthenticateUser authenticates the user identified by the given username
	//and password, and validates if the user has access to *any* tenant.
	AuthenticateUser(userName, password string) (Authorization, error)
	//AuthenticateUserFromCredentials authenticates the user identifed by the
	//given username and password, and validates if the user has access to the
	//given tenant. If an error is returned, it MUST be either ErrUnauthorized or
	//ErrForbidden from this package.
	AuthenticateUserInTenant(userName, password, tenantID string) (Authorization, error)
	//AuthenticateUserFromRequest reads credentials from the given incoming HTTP
	//request to authenticate the user which makes this request. The
	//implementation shall follow the conventions of the concrete backend, e.g. a
	//OAuth backend could try to read a Bearer token from the Authorization
	//header, whereas an OpenStack auth driver would look for a Keystone token in the
	//X-Auth-Token header.
	AuthenticateUserFromRequest(r *http.Request) (Authorization, error)
}

//Error types used by AuthDriver.
var (
	ErrUnauthorized = errors.New("Unauthorized")
	ErrForbidden    = errors.New("Forbidden")
)
