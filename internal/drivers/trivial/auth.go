// SPDX-FileCopyrightText: 2021 SAP SE
// SPDX-License-Identifier: Apache-2.0

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

const driverName = "trivial"

////////////////////////////////////////////////////////////////////////////////
// type userIdentity

type userIdentity struct {
	Username string
}

func (uid *userIdentity) PluginTypeID() string {
	return driverName
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
	return driverName
}

func (d *AuthDriver) Init(ctx context.Context, rc *redis.Client) error {
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
