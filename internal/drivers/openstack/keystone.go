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
package openstack

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/roles"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
	"github.com/gophercloud/utils/openstack/clientconfig"
	"github.com/sapcc/go-bits/gopherpolicy"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/keppel"
)

type keystoneDriver struct {
	Provider       *gophercloud.ProviderClient
	IdentityV3     *gophercloud.ServiceClient
	TokenValidator *gopherpolicy.TokenValidator
	ServiceUserID  string
	LocalRoleID    string
}

func init() {
	keppel.RegisterAuthDriver("keystone", func() (keppel.AuthDriver, error) {
		mustGetenv := func(key string) string {
			val := os.Getenv(key)
			if val == "" {
				logg.Fatal("missing environment variable: %s", key)
			}
			return val
		}

		//authenticate service user
		ao, err := clientconfig.AuthOptions(nil)
		if err != nil {
			return nil, errors.New("cannot find OpenStack credentials: " + err.Error())
		}
		ao.AllowReauth = true
		provider, err := openstack.AuthenticatedClient(*ao)
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
			ServiceUserID:  serviceUser.ID,
			LocalRoleID:    localRole.ID,
		}, nil
	})
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
	client, err := openstack.NewIdentityV3(
		requesterToken.ProviderClient, gophercloud.EndpointOpts{})
	if err != nil {
		return err
	}
	result := roles.Assign(client, d.LocalRoleID, roles.AssignOpts{
		UserID:    d.ServiceUserID,
		ProjectID: account.AuthTenantID,
	})
	return result.Err
}

//possible formats for the username:
//
//		user@domain/project@domain
//		user@domain/project
//
var userNameRx = regexp.MustCompile(`^([^/@]+)@([^/@]+)/([^/@]+)(?:@([^/@]+))?$`)

//                                    ^------^ ^------^ ^------^    ^------^
//                                      user   u. dom.   project    pr. dom.

//AuthenticateUser implements the keppel.AuthDriver interface.
func (d *keystoneDriver) AuthenticateUser(userName, password string) (keppel.Authorization, *keppel.RegistryV2Error) {
	match := userNameRx.FindStringSubmatch(userName)
	if match == nil {
		return nil, keppel.ErrUnauthorized.With(`invalid username (expected "user@domain/project" or "user@domain/project@domain" format)`)
	}

	authOpts := gophercloud.AuthOptions{
		IdentityEndpoint: d.IdentityV3.Endpoint,
		Username:         match[1],
		DomainName:       match[2],
		Password:         password,
		Scope: &gophercloud.AuthScope{
			ProjectName: match[3],
			DomainName:  match[4],
		},
	}
	if authOpts.Scope.DomainName == "" {
		authOpts.Scope.DomainName = authOpts.DomainName
	}

	//use a fresh ServiceClient for tokens.Create(): otherwise, a 401 is going to
	//confuse Gophercloud and make it refresh our own token although that's not
	//the problem
	client := *d.IdentityV3
	client.TokenID = ""
	client.EndpointLocator = nil
	client.ReauthFunc = nil

	result := tokens.Create(&client, &authOpts)
	t := d.TokenValidator.TokenFromGophercloudResult(result)
	if t.Err != nil {
		return nil, keppel.ErrUnauthorized.With(
			"failed to get token for user %q: %s",
			userName, t.Err.Error(),
		)
	}
	return &keystoneAuthorization{t}, nil
}

//AuthenticateUserFromRequest implements the keppel.AuthDriver interface.
func (d *keystoneDriver) AuthenticateUserFromRequest(r *http.Request) (keppel.Authorization, *keppel.RegistryV2Error) {
	t := d.TokenValidator.CheckToken(r)
	if t.Err != nil {
		return nil, keppel.ErrUnauthorized.With("X-Auth-Token validation failed: " + t.Err.Error())
	}

	t.Context.Logger = logg.Debug
	//token.Context.Request = mux.Vars(r) //not used at the moment

	if !t.Check("account:list") {
		return nil, keppel.ErrDenied.With("")
	}
	return keystoneAuthorization{t}, nil
}

type keystoneAuthorization struct {
	t *gopherpolicy.Token
}

var ruleForPerm = map[keppel.Permission]string{
	keppel.CanViewAccount:     "account:show",
	keppel.CanPullFromAccount: "account:pull",
	keppel.CanPushToAccount:   "account:push",
	keppel.CanChangeAccount:   "account:edit",
}

//HasPermission implements the keppel.Authorization interface.
func (a keystoneAuthorization) HasPermission(perm keppel.Permission, tenantID string) bool {
	a.t.Context.Request["account_project_id"] = tenantID
	return a.t.Check(ruleForPerm[perm])
}
