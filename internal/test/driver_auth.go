// SPDX-FileCopyrightText: 2018 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/audittools"

	"github.com/sapcc/keppel/internal/keppel"
)

// AuthDriver (driver ID "unittest") is a keppel.AuthDriver for unit tests.
type AuthDriver struct {
	// for AuthenticateUser
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
func (d *AuthDriver) Init(ctx context.Context, rc *redis.Client) error {
	return nil
}

// AuthenticateUser implements the keppel.AuthDriver interface.
func (d *AuthDriver) AuthenticateUser(ctx context.Context, userName, password string) (keppel.UserIdentity, *keppel.RegistryV2Error) {
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
		for field := range strings.SplitSeq(permsHeader, ",") {
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
	// return a dummy UserInfo to enable testing of audit events (a nil UserInfo
	// will suppress audit event generation)
	return dummyUserInfo{}
}

func (uid *userIdentity) SerializeToJSON() (payload []byte, err error) {
	return json.Marshal(uid)
}

func (uid *userIdentity) DeserializeFromJSON(in []byte, _ keppel.AuthDriver) error {
	return json.Unmarshal(in, &uid)
}

type dummyUserInfo struct{}

func (dummyUserInfo) AsInitiator(_ cadf.Host) cadf.Resource {
	return cadf.Resource{}
}
