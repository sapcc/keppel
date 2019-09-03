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

package test

import (
	"errors"
	"net/http"
	"strings"

	"github.com/sapcc/keppel/internal/keppel"
)

//A dummy auth driver that always returns errors.
type noopAuthDriver struct{}

func init() {
	keppel.RegisterAuthDriver("noop", func() (keppel.AuthDriver, error) { return &noopAuthDriver{}, nil })
}

func (*noopAuthDriver) ValidateTenantID(tenantID string) error {
	return nil
}

func (*noopAuthDriver) SetupAccount(account keppel.Account, an keppel.Authorization) error {
	return errors.New("SetupAccount not implemented for noopAuthDriver")
}

func (*noopAuthDriver) AuthenticateUser(userName, password string) (keppel.Authorization, *keppel.RegistryV2Error) {
	return nil, keppel.ErrUnsupported.With("AuthenticateUser not implemented for noopAuthDriver")
}

func (*noopAuthDriver) AuthenticateUserFromRequest(r *http.Request) (keppel.Authorization, *keppel.RegistryV2Error) {
	return nil, keppel.ErrUnsupported.With("AuthenticateUserFromRequest not implemented for noopAuthDriver")
}

////////////////////////////////////////////////////////////////////////////////

//AuthDriver (driver ID "unittest") is a keppel.AuthDriver for unit tests.
type AuthDriver struct {
	//for SetupAccount
	AccountsThatWereSetUp []keppel.Account
	//for AuthenticateUser
	ExpectedUserName   string
	ExpectedPassword   string
	GrantedPermissions string
}

func init() {
	keppel.RegisterAuthDriver("unittest", func() (keppel.AuthDriver, error) { return &AuthDriver{}, nil })
}

//ValidateTenantID implements the keppel.AuthDriver interface.
func (d *AuthDriver) ValidateTenantID(tenantID string) error {
	if tenantID == "invalid" {
		return errors.New(`must not be "invalid"`)
	}
	return nil
}

//SetupAccount implements the keppel.AuthDriver interface.
func (d *AuthDriver) SetupAccount(account keppel.Account, an keppel.Authorization) error {
	d.AccountsThatWereSetUp = append(d.AccountsThatWereSetUp, account)
	return nil
}

//AuthenticateUser implements the keppel.AuthDriver interface.
func (d *AuthDriver) AuthenticateUser(userName, password string) (keppel.Authorization, *keppel.RegistryV2Error) {
	is := func(a, b string) bool {
		return a != "" && a == b
	}
	if is(userName, d.ExpectedUserName) && is(password, d.ExpectedPassword) {
		return d.parseAuthorization(d.GrantedPermissions), nil
	}
	return nil, keppel.ErrUnauthorized.With("wrong credentials")
}

//AuthenticateUserFromRequest implements the keppel.AuthDriver interface.
func (d *AuthDriver) AuthenticateUserFromRequest(r *http.Request) (keppel.Authorization, *keppel.RegistryV2Error) {
	hdr := r.Header.Get("X-Test-Perms")
	if hdr == "" {
		return nil, keppel.ErrUnauthorized.With("missing X-Test-Perms header")
	}
	return d.parseAuthorization(hdr), nil
}

func (d *AuthDriver) parseAuthorization(permsHeader string) keppel.Authorization {
	perms := make(map[string]map[string]bool)
	if permsHeader != "" {
		for _, field := range strings.Split(permsHeader, ",") {
			fields := strings.SplitN(field, ":", 2)
			if _, ok := perms[fields[0]]; !ok {
				perms[fields[0]] = make(map[string]bool)
			}
			perms[fields[0]][fields[1]] = true
		}
	}
	return authorization{d.ExpectedUserName, perms}
}

type authorization struct {
	userName string
	perms    map[string]map[string]bool
}

func (a authorization) UserName() string {
	return a.userName
}

func (a authorization) HasPermission(perm keppel.Permission, tenantID string) bool {
	return a.perms[string(perm)][tenantID]
}
