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
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/go-redis/redis"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/keppel/internal/keppel"
)

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
	keppel.RegisterAuthDriver("unittest", func(*redis.Client) (keppel.AuthDriver, error) { return &AuthDriver{}, nil })
}

//DriverName implements the keppel.AuthDriver interface.
func (d *AuthDriver) DriverName() string {
	return "unittest"
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

func (a authorization) UserInfo() audittools.UserInfo {
	//return a dummy UserInfo to enable testing of audit events (a nil UserInfo
	//will suppress audit event generation)
	return dummyUserInfo{}
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

func (dummyUserInfo) DomainScopeUUID() string {
	return ""
}

var authorizationHeader = "Basic " + base64.StdEncoding.EncodeToString(
	[]byte("correctusername:correctpassword"),
)

//GetTokenForTest can be used by unit tests to obtain a token for use with the
//Registry v2 API. `h` must serve an authapi.API that uses `d` as its auth
//driver.
//
//`scope` is the token scope, e.g. "repository:test1/foo:pull". `authTenantID`
//is the ID of the auth tenant backing the requested account. `perms` is the
//set of permissions that the requesting user has (the AuthDriver will set up
//mock permissions for the duration of the token request).
func (d *AuthDriver) GetTokenForTest(t *testing.T, h http.Handler, service, scope, authTenantID string, perms ...keppel.Permission) string {
	t.Helper()
	//configure AuthDriver to allow access for this call
	d.ExpectedUserName = "correctusername"
	d.ExpectedPassword = "correctpassword"
	permStrs := make([]string, len(perms))
	for idx, perm := range perms {
		permStrs[idx] = string(perm) + ":" + authTenantID
	}
	d.GrantedPermissions = strings.Join(permStrs, ",")

	//build a token request
	query := url.Values{}
	query.Set("service", service)
	if scope != "" {
		query.Set("scope", scope)
	}
	_, bodyBytes := assert.HTTPRequest{
		Method: "GET",
		Path:   "/keppel/v1/auth?" + query.Encode(),
		Header: map[string]string{
			"Authorization":     authorizationHeader,
			"X-Forwarded-Host":  service,
			"X-Forwarded-Proto": "https",
		},
		ExpectStatus: http.StatusOK,
	}.Check(t, h)

	var data struct {
		Token string `json:"token"`
	}
	err := json.Unmarshal(bodyBytes, &data)
	if err != nil {
		t.Fatal(err.Error())
	}
	return data.Token
}
