/******************************************************************************
*
*  Copyright 2021 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package trivial

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"

	"github.com/sapcc/keppel/internal/keppel"

	"github.com/go-redis/redis/v8"
	"github.com/sapcc/go-bits/audittools"
)

type AuthDriver struct {
	userName string
	password string
}

func init() {
	keppel.RegisterUserIdentity("trivial", deserializeTrivialUserIdentity)
	keppel.RegisterAuthDriver("trivial", func(rc *redis.Client) (keppel.AuthDriver, error) {
		userName := os.Getenv("KEPPEL_USERNAME")
		if userName == "" {
			return &AuthDriver{}, errors.New("KEPPEL_USERNAME env cannot be empty when using trivial auth")
		}

		password := os.Getenv("KEPPEL_PASSWORD")
		if password == "" {
			return nil, errors.New("KEPPEL_PASSWORD env cannot be empty when using trivial auth")
		}
		return &AuthDriver{userName: userName, password: password}, nil
	})
}

func deserializeTrivialUserIdentity(in []byte, _ keppel.AuthDriver) (keppel.UserIdentity, error) {
	var uid userIdentity
	err := json.Unmarshal(in, &uid)
	return uid, err
}

type userIdentity struct {
	Username string
}

func (uid userIdentity) HasPermission(perm keppel.Permission, tenantID string) bool {
	return true
}

type DummyUserInfo struct {
	Username string
}

func (DummyUserInfo) UserUUID() string {
	return ""
}

func (dui DummyUserInfo) UserName() string {
	return dui.Username
}

func (DummyUserInfo) UserDomainName() string {
	return ""
}

func (DummyUserInfo) ProjectScopeUUID() string {
	return ""
}

func (DummyUserInfo) ProjectScopeName() string {
	return ""
}

func (DummyUserInfo) ProjectScopeDomainName() string {
	return ""
}

func (DummyUserInfo) DomainScopeUUID() string {
	return ""
}

func (DummyUserInfo) DomainScopeName() string {
	return ""
}

func (uid userIdentity) UserInfo() audittools.UserInfo {
	return DummyUserInfo(uid)
}

func (uid userIdentity) UserName() string {
	return uid.Username
}

func (uid userIdentity) UserType() keppel.UserType {
	return keppel.RegularUser
}

func (uid userIdentity) SerializeToJSON() (typeName string, payload []byte, err error) {
	payload, err = json.Marshal(uid)
	return "trivial", payload, err
}

func (d *AuthDriver) AuthenticateUser(userName, password string) (keppel.UserIdentity, *keppel.RegistryV2Error) {
	if d.userName == userName && d.password == password {
		return userIdentity{Username: userName}, nil
	}

	return nil, keppel.ErrUnauthorized.With(`invalid username or password`)
}

func (d *AuthDriver) AuthenticateUserFromRequest(r *http.Request) (keppel.UserIdentity, *keppel.RegistryV2Error) {
	return userIdentity{Username: d.userName}, nil
}

func (d *AuthDriver) DriverName() string {
	return "trivial"
}

func (d *AuthDriver) ValidateTenantID(tenantID string) error {
	return nil
}
