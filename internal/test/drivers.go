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
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/sapcc/keppel/internal/keppel"
)

//Implements all three Driver interfaces. All methods return errors or empty
//values all the time, except for initialization methods (ReadConfig, Connect)
//which return nil.
type noopDriver struct{}

func init() {
	keppel.RegisterAuthDriver("noop", func() (keppel.AuthDriver, error) { return &noopDriver{}, nil })
	keppel.RegisterOrchestrationDriver("noop", func(keppel.StorageDriver, keppel.Configuration, keppel.DBAccessForOrchestrationDriver) (keppel.OrchestrationDriver, error) {
		return &noopDriver{}, nil
	})
	keppel.RegisterStorageDriver("noop", func(keppel.AuthDriver, keppel.Configuration) (keppel.StorageDriver, error) { return &noopDriver{}, nil })
}

func (*noopDriver) ValidateTenantID(tenantID string) error {
	return nil
}

func (*noopDriver) SetupAccount(account keppel.Account, an keppel.Authorization) error {
	return errors.New("SetupAccount not implemented for NoopDriver")
}

func (*noopDriver) AuthenticateUser(userName, password string) (keppel.Authorization, *keppel.RegistryV2Error) {
	return nil, keppel.ErrUnsupported.With("AuthenticateUser not implemented for NoopDriver")
}

func (*noopDriver) AuthenticateUserFromRequest(r *http.Request) (keppel.Authorization, *keppel.RegistryV2Error) {
	return nil, keppel.ErrUnsupported.With("AuthenticateUserFromRequest not implemented for NoopDriver")
}

func (*noopDriver) GetEnvironment(account keppel.Account) (map[string]string, error) {
	return nil, errors.New("GetEnvironment not implemented for NoopDriver")
}

func (*noopDriver) DoHTTPRequest(account keppel.Account, r *http.Request) (*http.Response, error) {
	return nil, errors.New("DoHTTPRequest not implemented for NoopDriver")
}

func (*noopDriver) Run(ctx context.Context) (ok bool) {
	return false
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
		return parseAuthorization(d.GrantedPermissions), nil
	}
	return nil, keppel.ErrUnauthorized.With("wrong credentials")
}

//AuthenticateUserFromRequest implements the keppel.AuthDriver interface.
func (d *AuthDriver) AuthenticateUserFromRequest(r *http.Request) (keppel.Authorization, *keppel.RegistryV2Error) {
	hdr := r.Header.Get("X-Test-Perms")
	if hdr == "" {
		return nil, keppel.ErrUnauthorized.With("missing X-Test-Perms header")
	}
	return parseAuthorization(hdr), nil
}

func parseAuthorization(permsHeader string) keppel.Authorization {
	perms := make(map[string]map[string]bool)
	for _, field := range strings.Split(permsHeader, ",") {
		fields := strings.SplitN(field, ":", 2)
		if _, ok := perms[fields[0]]; !ok {
			perms[fields[0]] = make(map[string]bool)
		}
		perms[fields[0]][fields[1]] = true
	}
	return authorization{perms}
}

type authorization struct {
	perms map[string]map[string]bool
}

func (a authorization) HasPermission(perm keppel.Permission, tenantID string) bool {
	return a.perms[string(perm)][tenantID]
}
