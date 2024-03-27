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
	"context"
	"encoding/json"
	"net/http"

	"github.com/sapcc/keppel/internal/keppel"

	"github.com/redis/go-redis/v9"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/osext"
)

func init() {
	keppel.AuthDriverRegistry.Add(func() keppel.AuthDriver { return &AuthDriver{} })
	keppel.UserIdentityRegistry.Add(func() keppel.UserIdentity { return &userIdentity{} })
}

////////////////////////////////////////////////////////////////////////////////
// type userIdentity

type userIdentity struct {
	Username string
}

func (uid *userIdentity) PluginTypeID() string {
	return "trivial"
}

func (uid *userIdentity) HasPermission(perm keppel.Permission, tenantID string) bool {
	return tenantID != ""
}

func (uid *userIdentity) UserInfo() audittools.UserInfo {
	return nil
}

func (uid *userIdentity) UserName() string {
	return uid.Username
}

func (uid *userIdentity) UserType() keppel.UserType {
	return keppel.RegularUser
}

func (uid *userIdentity) SerializeToJSON() (payload []byte, err error) {
	return json.Marshal(uid.Username)
}

func (uid *userIdentity) DeserializeFromJSON(in []byte, _ keppel.AuthDriver) error {
	return json.Unmarshal(in, &uid.Username)
}

////////////////////////////////////////////////////////////////////////////////
// type AuthDriver

type AuthDriver struct {
	userName string
	password string
}

func (d *AuthDriver) PluginTypeID() string {
	return "trivial"
}

func (d *AuthDriver) Init(rc *redis.Client) error {
	d.userName = osext.MustGetenv("KEPPEL_USERNAME")
	d.password = osext.MustGetenv("KEPPEL_PASSWORD")
	return nil
}

func (d *AuthDriver) AuthenticateUser(ctx context.Context, userName, password string) (keppel.UserIdentity, *keppel.RegistryV2Error) {
	if d.userName == userName && d.password == password {
		return &userIdentity{Username: userName}, nil
	}

	return nil, keppel.ErrUnauthorized.With(`invalid username or password`)
}

func (d *AuthDriver) AuthenticateUserFromRequest(r *http.Request) (keppel.UserIdentity, *keppel.RegistryV2Error) {
	if r.Header.Get("Authorization") == "" {
		// fallback to anonymous auth
		return nil, nil
	}

	return &userIdentity{Username: d.userName}, nil
}
