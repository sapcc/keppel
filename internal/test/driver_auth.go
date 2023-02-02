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
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/sapcc/go-bits/audittools"

	"github.com/sapcc/keppel/internal/keppel"
)

// AuthDriver (driver ID "unittest") is a keppel.AuthDriver for unit tests.
type AuthDriver struct {
	//for AuthenticateUser
	ExpectedUserName   string
	ExpectedPassword   string
	GrantedPermissions string
}

func init() {
	keppel.AuthDriverRegistry.Add(func() keppel.AuthDriver { return &AuthDriver{} })
	keppel.UserIdentityRegistry.Add(func() keppel.UserIdentity { return &userIdentity{} })
}

// PluginTypeID implements the keppel.AuthDriver interface.
func (d *AuthDriver) PluginTypeID() string {
	return "unittest"
}

// Init implements the keppel.AuthDriver interface.
func (d *AuthDriver) Init(rc *redis.Client) error {
	return nil
}

// ValidateTenantID implements the keppel.AuthDriver interface.
func (d *AuthDriver) ValidateTenantID(tenantID string) error {
	if tenantID == "invalid" {
		return errors.New(`must not be "invalid"`)
	}
	return nil
}

// AuthenticateUser implements the keppel.AuthDriver interface.
func (d *AuthDriver) AuthenticateUser(userName, password string) (keppel.UserIdentity, *keppel.RegistryV2Error) {
	is := func(a, b string) bool {
		return a != "" && a == b
	}
	if is(userName, d.ExpectedUserName) && is(password, d.ExpectedPassword) {
		return d.parseUserIdentity(d.GrantedPermissions), nil
	}
	return nil, keppel.ErrUnauthorized.With("wrong credentials")
}

// AuthenticateUserFromRequest implements the keppel.AuthDriver interface.
func (d *AuthDriver) AuthenticateUserFromRequest(r *http.Request) (keppel.UserIdentity, *keppel.RegistryV2Error) {
	hdr := r.Header.Get("X-Test-Perms")
	if hdr == "" {
		return nil, nil
	}
	return d.parseUserIdentity(hdr), nil
}

func (d *AuthDriver) parseUserIdentity(permsHeader string) keppel.UserIdentity {
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
	return &userIdentity{d.ExpectedUserName, perms}
}

type userIdentity struct {
	Username string
	Perms    map[string]map[string]bool
}

func (uid *userIdentity) PluginTypeID() string {
	return "unittest"
}

func (uid *userIdentity) UserName() string {
	return uid.Username
}

func (uid *userIdentity) HasPermission(perm keppel.Permission, tenantID string) bool {
	return uid.Perms[string(perm)][tenantID]
}

func (uid *userIdentity) UserType() keppel.UserType {
	return keppel.RegularUser
}

func (uid *userIdentity) UserInfo() audittools.UserInfo {
	//return a dummy UserInfo to enable testing of audit events (a nil UserInfo
	//will suppress audit event generation)
	return dummyUserInfo{}
}

func (uid *userIdentity) SerializeToJSON() (payload []byte, err error) {
	return json.Marshal(uid)
}

func (uid *userIdentity) DeserializeFromJSON(in []byte, _ keppel.AuthDriver) error {
	return json.Unmarshal(in, &uid)
}

type dummyUserInfo struct{}

func (dummyUserInfo) UserUUID() string {
	return "dummy-userid"
}

func (dummyUserInfo) UserName() string {
	return "dummy-username"
}

func (dummyUserInfo) UserDomainName() string {
	return "dummy-domainname"
}

func (dummyUserInfo) ProjectScopeUUID() string {
	return "dummy-projectid"
}

func (dummyUserInfo) ProjectScopeName() string {
	return "dummy-projectname"
}

func (dummyUserInfo) ProjectScopeDomainName() string {
	return "dummy-projectdomainname"
}

func (dummyUserInfo) DomainScopeUUID() string {
	return ""
}

func (dummyUserInfo) DomainScopeName() string {
	return ""
}

func (dummyUserInfo) ApplicationCredentialID() string {
	return ""
}
