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
	"net/http"

	"github.com/sapcc/keppel/internal/keppel"

	"github.com/go-redis/redis/v8"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/osext"
)

type AuthDriver struct {
	userName string
	password string
}

func init() {
	keppel.RegisterUserIdentity("trivial", deserializeTrivialUserIdentity)
	keppel.RegisterAuthDriver("trivial", func(rc *redis.Client) (keppel.AuthDriver, error) {
		return &AuthDriver{
			userName: osext.MustGetenv("KEPPEL_USERNAME"),
			password: osext.MustGetenv("KEPPEL_PASSWORD"),
		}, nil
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

func (uid userIdentity) UserInfo() audittools.UserInfo {
	return nil
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
	if r.Header.Get("Authorization") == "" {
		return nil, nil
	}
	return userIdentity{Username: d.userName}, nil
}

func (d *AuthDriver) DriverName() string {
	return "trivial"
}

func (d *AuthDriver) ValidateTenantID(tenantID string) error {
	return nil
}
