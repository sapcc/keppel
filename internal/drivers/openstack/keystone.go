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

// Package openstack contains:
//
// - the AuthDriver "keystone": Keppel tenants are Keystone projects. Incoming HTTP requests are authenticated by reading a Keystone token from the X-Auth-Token request header.
//
// - the StorageDriver "swift": Data for a Keppel account is stored in the Swift container "keppel-<accountname>" in the tenant's Swift account.
//
// - the NameClaimDriver "openstack-basic": A static whitelist is used to check which project can claim which account names.
package openstack

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	policy "github.com/databus23/goslo.policy"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/gophercloud/utils/v2/openstack/clientconfig"
	"github.com/redis/go-redis/v9"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/errext"
	"github.com/sapcc/go-bits/gopherpolicy"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/osext"

	"github.com/sapcc/keppel/internal/keppel"
)

type keystoneDriver struct {
	Provider       *gophercloud.ProviderClient
	IdentityV3     *gophercloud.ServiceClient
	TokenValidator *gopherpolicy.TokenValidator
	IsRelevantRole map[string]bool
}

func init() {
	keppel.AuthDriverRegistry.Add(func() keppel.AuthDriver { return &keystoneDriver{} })
	keppel.UserIdentityRegistry.Add(func() keppel.UserIdentity { return &keystoneUserIdentity{} })
}

// PluginTypeID implements the keppel.AuthDriver interface.
func (d *keystoneDriver) PluginTypeID() string {
	return "keystone"
}

// Init implements the keppel.AuthDriver interface.
func (d *keystoneDriver) Init(ctx context.Context, rc *redis.Client) error {
	// authenticate service user
	ao, err := clientconfig.AuthOptions(nil)
	if err != nil {
		return errors.New("cannot find OpenStack credentials: " + err.Error())
	}
	ao.AllowReauth = true
	d.Provider, err = openstack.AuthenticatedClient(ctx, *ao)
	if err != nil {
		return errors.New("cannot connect to OpenStack: " + err.Error())
	}

	// find Identity V3 endpoint
	eo := gophercloud.EndpointOpts{
		// note that empty values are acceptable in both fields
		Region:       os.Getenv("OS_REGION_NAME"),
		Availability: gophercloud.Availability(os.Getenv("OS_INTERFACE")),
	}
	d.IdentityV3, err = openstack.NewIdentityV3(d.Provider, eo)
	if err != nil {
		return errors.New("cannot find Keystone V3 API: " + err.Error())
	}

	// load oslo.policy
	d.TokenValidator = &gopherpolicy.TokenValidator{IdentityV3: d.IdentityV3}
	policyFilePath := osext.MustGetenv("KEPPEL_OSLO_POLICY_PATH")
	err = d.TokenValidator.LoadPolicyFile(policyFilePath)
	if err != nil {
		return err
	}
	if rc == nil {
		d.TokenValidator.Cacher = gopherpolicy.InMemoryCacher()
	} else {
		d.TokenValidator.Cacher = redisCacher{rc}
	}

	// read policy file "manually" to find which Keystone roles are relevant to us
	d.IsRelevantRole = make(map[string]bool)
	roleCheckRx := regexp.MustCompile(`role:([a-zA-Z0-9_-]+)`)
	buf, err := os.ReadFile(policyFilePath)
	if err != nil {
		return err
	}
	for _, match := range roleCheckRx.FindAllStringSubmatch(string(buf), -1) {
		roleName := match[1]
		d.IsRelevantRole[roleName] = true
	}

	return nil
}

// AuthenticateUser implements the keppel.AuthDriver interface.
func (d *keystoneDriver) AuthenticateUser(ctx context.Context, userName, password string) (keppel.UserIdentity, *keppel.RegistryV2Error) {
	authOpts, rerr := parseUserNameAndPassword(userName, password)
	if rerr != nil {
		return nil, rerr
	}
	authOpts.IdentityEndpoint = d.IdentityV3.Endpoint
	authOpts.AllowReauth = false

	// abort the authentication after 45 seconds if it's stuck; we want to be able
	// to show a useful error message before we run into our own timeouts (usually
	// the loadbalancer or whatever's in front of us will have a timeout of 60
	// seconds)
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel() // silence govet

	// perform the authentication with a fresh ServiceClient, otherwise a 401
	// response will trigger a useless reauthentication of the service user
	throwAwayClient := gophercloud.ServiceClient{
		ProviderClient: &gophercloud.ProviderClient{Throwaway: true},
		Endpoint:       d.IdentityV3.Endpoint,
	}
	throwAwayClient.SetThrowaway(true)
	throwAwayClient.ReauthFunc = nil
	throwAwayClient.SetTokenAndAuthResult(nil) //nolint:errcheck

	t := d.TokenValidator.CheckCredentials(
		ctx, fmt.Sprintf("username=%s,password=%s", userName, password),
		func() gopherpolicy.TokenResult { return tokens.Create(ctx, &throwAwayClient, &authOpts) },
	)

	if t.Err != nil {
		if err, ok := errext.As[gophercloud.ErrUnexpectedResponseCode](t.Err); ok && err.Actual == http.StatusTooManyRequests {
			retryAfterStr := err.ResponseHeader.Get("Retry-After")
			return nil, keppel.ErrTooManyRequests.With("").WithHeader("Retry-After", retryAfterStr)
		}
		return nil, keppel.ErrUnauthorized.With(
			"failed to get token for user %q: %s",
			userName, t.Err.Error(),
		)
	}
	return newKeystoneUserIdentity(t, d.IsRelevantRole), nil
}

// possible formats for the username:
//
//	${USER}@${DOMAIN}/${PROJECT}@${DOMAIN}
//	${USER}@${DOMAIN}/${PROJECT}
//
//	applicationcredential-${APPLICATION_CREDENTIAL_ID}
var userNameRx = regexp.MustCompile(`^([^/@]+)@([^/@]+)/([^/@]+)(?:@([^/@]+))?$`)

//                                    ^------^ ^------^ ^------^    ^------^
//                                      user   u. dom.   project    pr. dom.

func parseUserNameAndPassword(userName, password string) (tokens.AuthOptions, *keppel.RegistryV2Error) {
	if strings.HasPrefix(userName, "applicationcredential-") {
		return tokens.AuthOptions{
			ApplicationCredentialID:     strings.TrimPrefix(userName, "applicationcredential-"),
			ApplicationCredentialSecret: password,
		}, nil
	}

	match := userNameRx.FindStringSubmatch(userName)
	if match == nil {
		return tokens.AuthOptions{}, keppel.ErrUnauthorized.With(`invalid username (expected "user@domain/project" or "user@domain/project@domain" format)`)
	}

	ao := tokens.AuthOptions{
		Username:   match[1],
		DomainName: match[2],
		Password:   password,
		Scope: tokens.Scope{
			ProjectName: match[3],
			DomainName:  match[4],
		},
	}
	if ao.Scope.DomainName == "" {
		ao.Scope.DomainName = ao.DomainName
	}
	return ao, nil
}

// AuthenticateUserFromRequest implements the keppel.AuthDriver interface.
func (d *keystoneDriver) AuthenticateUserFromRequest(r *http.Request) (keppel.UserIdentity, *keppel.RegistryV2Error) {
	if r.Header.Get("X-Auth-Token") == "" {
		// fallback to anonymous auth
		return nil, nil
	}

	t := d.TokenValidator.CheckToken(r)
	if t.Err != nil {
		return nil, keppel.ErrUnauthorized.With("X-Auth-Token validation failed: " + t.Err.Error())
	}

	// t.Context.Request = mux.Vars(r) //not used at the moment

	a := newKeystoneUserIdentity(t, d.IsRelevantRole)
	if !a.t.Check("account:list") {
		return nil, keppel.ErrDenied.With("").WithStatus(http.StatusForbidden)
	}
	return a, nil
}

type keystoneUserIdentity struct {
	t *gopherpolicy.Token
	// ^ WARNING: Token may not always contain everything you expect
	// because of a serialization roundtrip. See SerializeToJSON() and
	// DeserializeFromJSON() for details.
}

func newKeystoneUserIdentity(t *gopherpolicy.Token, isRelevantRole map[string]bool) *keystoneUserIdentity {
	// to compress the serialization of this keystoneUserIdentity, remove all
	// irrelevant roles from the token payload ("relevant" means that this role
	// is tested for in our policy rules)
	var filteredRoles []string
	for _, role := range t.Context.Roles {
		if isRelevantRole[role] {
			filteredRoles = append(filteredRoles, role)
		}
	}
	t.Context.Roles = filteredRoles

	return &keystoneUserIdentity{t}
}

var ruleForPerm = map[keppel.Permission]string{
	keppel.CanViewAccount:       "account:show",
	keppel.CanPullFromAccount:   "account:pull",
	keppel.CanPushToAccount:     "account:push",
	keppel.CanDeleteFromAccount: "account:delete",
	keppel.CanChangeAccount:     "account:edit",
	keppel.CanViewQuotas:        "quota:show",
	keppel.CanChangeQuotas:      "quota:edit",
}

// PluginTypeID implements the keppel.UserIdentity interface.
func (a *keystoneUserIdentity) PluginTypeID() string { return "keystone" }

// UserName implements the keppel.UserIdentity interface.
func (a *keystoneUserIdentity) UserName() string {
	return fmt.Sprintf("%s@%s/%s@%s",
		a.t.UserName(),
		a.t.UserDomainName(),
		a.t.ProjectScopeName(),
		a.t.ProjectScopeDomainName(),
	)
}

// HasPermission implements the keppel.UserIdentity interface.
func (a *keystoneUserIdentity) HasPermission(perm keppel.Permission, tenantID string) bool {
	if tenantID == "" {
		return false
	}

	a.t.Context.Request["target.project.id"] = tenantID
	logg.Debug("token has object attributes = %v", a.t.Context.Request)

	rule, hasRule := ruleForPerm[perm]
	if !hasRule {
		return false
	}

	result := a.t.Check(rule)
	logg.Debug("policy rule %q evaluates to %t", rule, result)
	return result
}

// UserType implements the keppel.UserIdentity interface.
func (a *keystoneUserIdentity) UserType() keppel.UserType {
	return keppel.RegularUser
}

// UserInfo implements the keppel.UserIdentity interface.
func (a *keystoneUserIdentity) UserInfo() audittools.UserInfo {
	return a.t
}

// SerializeToJSON implements the keppel.UserIdentity interface.
func (a *keystoneUserIdentity) SerializeToJSON() (payload []byte, err error) {
	// We cannot serialize the entire gopherpolicy.Token, that would include the
	// X-Auth-Token and possibly even the full token response including service
	// catalog, and thus produce a rather massive payload. We skip the token and
	// token response and only serialize what we need to make policy decisions and
	// satisfy the audittools.UserInfo interface.
	return gopherpolicy.SerializeCompactContextToJSON(a.t.Context)
}

// DeserializeFromJSON implements the keppel.UserIdentity interface.
func (a *keystoneUserIdentity) DeserializeFromJSON(in []byte, ad keppel.AuthDriver) (err error) {
	d, ok := ad.(*keystoneDriver)
	if !ok {
		return keppel.ErrAuthDriverMismatch
	}

	a.t = &gopherpolicy.Token{
		Enforcer:       d.TokenValidator.Enforcer,
		Context:        policy.Context{}, // see below
		ProviderClient: nil,              // cannot be reasonably serialized; see comment above
		Err:            nil,
	}
	a.t.Context, err = gopherpolicy.DeserializeCompactContextFromJSON(in)
	return err
}
