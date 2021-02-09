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

//Package openstack contains:
//
//- the AuthDriver "keystone": Keppel tenants are Keystone projects. Incoming HTTP requests are authenticated by reading a Keystone token from the X-Auth-Token request header.
//
//- the StorageDriver "swift": Data for a Keppel account is stored in the Swift container "keppel-<accountname>" in the tenant's Swift account.
//
//- the NameClaimDriver "openstack-basic": A static whitelist is used to check which project can claim which account names.
package openstack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	policy "github.com/databus23/goslo.policy"
	"github.com/go-redis/redis"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/roles"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
	"github.com/gophercloud/utils/openstack/clientconfig"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/gopherpolicy"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/keppel"
)

type keystoneDriver struct {
	Provider       *gophercloud.ProviderClient
	IdentityV3     *gophercloud.ServiceClient
	TokenValidator *gopherpolicy.TokenValidator
	ServiceUser    tokens.User
	LocalRoleID    string
}

func init() {
	keppel.RegisterAuthorization("keystone", deserializeKeystoneAuthorization)
	keppel.RegisterAuthDriver("keystone", func(rc *redis.Client) (keppel.AuthDriver, error) {
		//authenticate service user
		ao, err := clientconfig.AuthOptions(nil)
		if err != nil {
			return nil, errors.New("cannot find OpenStack credentials: " + err.Error())
		}
		ao.AllowReauth = true
		provider, err := createProviderClient(*ao)
		if err != nil {
			return nil, errors.New("cannot connect to OpenStack: " + err.Error())
		}

		//find Identity V3 endpoint
		eo := gophercloud.EndpointOpts{
			//note that empty values are acceptable in both fields
			Region:       os.Getenv("OS_REGION_NAME"),
			Availability: gophercloud.Availability(os.Getenv("OS_INTERFACE")),
		}
		identityV3, err := openstack.NewIdentityV3(provider, eo)
		if err != nil {
			return nil, errors.New("cannot find Keystone V3 API: " + err.Error())
		}

		//load oslo.policy
		tv := &gopherpolicy.TokenValidator{IdentityV3: identityV3}
		err = tv.LoadPolicyFile(mustGetenv("KEPPEL_OSLO_POLICY_PATH"))
		if err != nil {
			return nil, err
		}
		if rc == nil {
			tv.Cacher = gopherpolicy.InMemoryCacher()
		} else {
			tv.Cacher = redisCacher{rc}
		}

		//resolve KEPPEL_AUTH_LOCAL_ROLE name into ID
		localRoleName := mustGetenv("KEPPEL_AUTH_LOCAL_ROLE")
		localRole, err := getRoleByName(identityV3, localRoleName)
		if err != nil {
			return nil, fmt.Errorf("cannot find Keystone role '%s': %s", localRoleName, err.Error())
		}

		//get user ID for service user
		authResult, ok := provider.GetAuthResult().(tokens.CreateResult)
		if !ok {
			return nil, fmt.Errorf("got unexpected auth result: %T", provider.GetAuthResult())
		}
		serviceUser, err := authResult.ExtractUser()
		if err != nil {
			return nil, errors.New("cannot extract own user metadata from token response: " + err.Error())
		}

		return &keystoneDriver{
			Provider:       provider,
			IdentityV3:     identityV3,
			TokenValidator: tv,
			ServiceUser:    *serviceUser,
			LocalRoleID:    localRole.ID,
		}, nil
	})
}

func mustGetenv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		logg.Fatal("missing environment variable: %s", key)
	}
	return val
}

func createProviderClient(ao gophercloud.AuthOptions) (*gophercloud.ProviderClient, error) {
	provider, err := openstack.NewClient(ao.IdentityEndpoint)
	if err == nil {
		//use http.DefaultClient, esp. to pick up the KEPPEL_INSECURE flag
		provider.HTTPClient = *http.DefaultClient
		err = openstack.Authenticate(provider, ao)
	}
	return provider, err
}

func getRoleByName(identityV3 *gophercloud.ServiceClient, name string) (roles.Role, error) {
	page, err := roles.List(identityV3, roles.ListOpts{Name: name}).AllPages()
	if err != nil {
		return roles.Role{}, err
	}
	list, err := roles.ExtractRoles(page)
	if err != nil {
		return roles.Role{}, err
	}
	if len(list) == 0 {
		return roles.Role{}, errors.New("no such role")
	}
	return list[0], nil
}

//DriverName implements the keppel.AuthDriver interface.
func (d *keystoneDriver) DriverName() string {
	return "keystone"
}

//ValidateTenantID implements the keppel.AuthDriver interface.
func (d *keystoneDriver) ValidateTenantID(tenantID string) error {
	if tenantID == "" {
		return errors.New("may not be empty")
	}
	return nil
}

//SetupAccount implements the keppel.AuthDriver interface.
func (d *keystoneDriver) SetupAccount(account keppel.Account, authorization keppel.Authorization) error {
	requesterToken := authorization.(keystoneAuthorization).t //is a *gopherpolicy.Token
	if requesterToken.ProviderClient == nil {
		return errors.New("user token does not contain a functional client (probably because of a serialization roundtrip)")
	}

	client, err := openstack.NewIdentityV3(
		requesterToken.ProviderClient, gophercloud.EndpointOpts{})
	if err != nil {
		return err
	}
	result := roles.Assign(client, d.LocalRoleID, roles.AssignOpts{
		UserID:    d.ServiceUser.ID,
		ProjectID: account.AuthTenantID,
	})
	return result.Err
}

//AuthenticateUser implements the keppel.AuthDriver interface.
func (d *keystoneDriver) AuthenticateUser(userName, password string) (keppel.Authorization, *keppel.RegistryV2Error) {
	authOpts, rerr := parseUserNameAndPassword(userName, password)
	if rerr != nil {
		return nil, rerr
	}
	authOpts.IdentityEndpoint = d.IdentityV3.Endpoint
	authOpts.AllowReauth = false

	//abort the authentication after 45 seconds if it's stuck; we want to be able
	//to show a useful error message before we run into our own timeouts (usually
	//the loadbalancer or whatever's in front of us will have a timeout of 60
	//seconds)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel() //silence govet

	//perform the authentication with a fresh ServiceClient, otherwise a 401
	//response will trigger a useless reauthentication of the service user
	throwAwayClient := gophercloud.ServiceClient{
		ProviderClient: &gophercloud.ProviderClient{
			HTTPClient: *http.DefaultClient,
			Throwaway:  true,
			Context:    ctx,
		},
		Endpoint: d.IdentityV3.Endpoint,
	}
	throwAwayClient.SetThrowaway(true)
	throwAwayClient.ReauthFunc = nil
	throwAwayClient.SetTokenAndAuthResult(nil)

	t := d.TokenValidator.CheckCredentials(
		fmt.Sprintf("username=%s,password=%s", userName, password),
		func() gopherpolicy.TokenResult { return tokens.Create(&throwAwayClient, &authOpts) },
	)

	if t.Err != nil {
		if err, ok := t.Err.(gophercloud.ErrDefault429); ok {
			retryAfterStr := err.ResponseHeader.Get("Retry-After")
			return nil, keppel.ErrTooManyRequests.With("").WithHeader("Retry-After", retryAfterStr)
		}
		return nil, keppel.ErrUnauthorized.With(
			"failed to get token for user %q: %s",
			userName, t.Err.Error(),
		)
	}
	return newKeystoneAuthorization(t), nil
}

//possible formats for the username:
//
//		${USER}@${DOMAIN}/${PROJECT}@${DOMAIN}
//		${USER}@${DOMAIN}/${PROJECT}
//
//		applicationcredential-${APPLICATION_CREDENTIAL_ID}
//
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

//AuthenticateUserFromRequest implements the keppel.AuthDriver interface.
func (d *keystoneDriver) AuthenticateUserFromRequest(r *http.Request) (keppel.Authorization, *keppel.RegistryV2Error) {
	t := d.TokenValidator.CheckToken(r)
	if t.Err != nil {
		return nil, keppel.ErrUnauthorized.With("X-Auth-Token validation failed: " + t.Err.Error())
	}

	//t.Context.Request = mux.Vars(r) //not used at the moment

	a := newKeystoneAuthorization(t)
	if !a.t.Check("account:list") {
		return nil, keppel.ErrDenied.With("").WithStatus(http.StatusForbidden)
	}
	return a, nil
}

type keystoneAuthorization struct {
	t *gopherpolicy.Token
	//^ WARNING: Token may not always contain everything you expect
	//because of a serialization roundtrip. See SerializeToJSON() and
	//deserializeKeystoneAuthorization() for details.
}

func newKeystoneAuthorization(t *gopherpolicy.Token) keystoneAuthorization {
	t.Context.Logger = logg.Debug
	logg.Debug("token has auth = %v", t.Context.Auth)
	logg.Debug("token has roles = %v", t.Context.Roles)
	return keystoneAuthorization{t}
}

var ruleForPerm = map[keppel.Permission]string{
	keppel.CanViewAccount:        "account:show",
	keppel.CanPullFromAccount:    "account:pull",
	keppel.CanPushToAccount:      "account:push",
	keppel.CanDeleteFromAccount:  "account:delete",
	keppel.CanChangeAccount:      "account:edit",
	keppel.CanViewQuotas:         "quota:show",
	keppel.CanChangeQuotas:       "quota:edit",
	keppel.CanAdministrateKeppel: "keppel:admin",
}

//UserName implements the keppel.Authorization interface.
func (a keystoneAuthorization) UserName() string {
	return fmt.Sprintf("%s@%s/%s@%s",
		a.t.UserName(),
		a.t.UserDomainName(),
		a.t.ProjectScopeName(),
		a.t.ProjectScopeDomainName(),
	)
}

//HasPermission implements the keppel.Authorization interface.
func (a keystoneAuthorization) HasPermission(perm keppel.Permission, tenantID string) bool {
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

//UserInfo implements the keppel.Authorization interface.
func (a keystoneAuthorization) UserInfo() audittools.UserInfo {
	return a.t
}

type serializedKeystoneAuthorization struct {
	Auth  map[string]string `json:"auth"`
	Roles []string          `json:"roles"`
}

//SerializeToJSON implements the keppel.Authorization interface.
func (a keystoneAuthorization) SerializeToJSON() (typeName string, payload []byte, err error) {
	//We cannot serialize the entire gopherpolicy.Token, that would include the
	//X-Auth-Token and possibly even the full token response including service
	//catalog, and thus produce a rather massive payload. We skip the token and
	//token response and only serialize what we need to make policy decisions and
	//satisfy the audittools.UserInfo interface.
	payload, err = json.Marshal(serializedKeystoneAuthorization{
		Auth:  a.t.Context.Auth,
		Roles: a.t.Context.Roles,
	})
	return "keystone", payload, err
}

func deserializeKeystoneAuthorization(in []byte, ad keppel.AuthDriver) (keppel.Authorization, error) {
	d, ok := ad.(*keystoneDriver)
	if !ok {
		return nil, keppel.ErrAuthDriverMismatch
	}

	var ska serializedKeystoneAuthorization
	err := json.Unmarshal(in, &ska)
	if err != nil {
		return nil, err
	}

	return newKeystoneAuthorization(&gopherpolicy.Token{
		Enforcer: d.TokenValidator.Enforcer,
		Context: policy.Context{
			Auth:    ska.Auth,
			Roles:   ska.Roles,
			Request: make(map[string]string), //filled by HasPermission(); does not need to be serialized
		},
		ProviderClient: nil, //cannot be reasonably serialized; see comment above
		Err:            nil,
	}), nil
}
