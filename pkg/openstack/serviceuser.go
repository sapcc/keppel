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

package openstack

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/roles"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
	"github.com/sapcc/go-bits/gopherpolicy"
	"github.com/sapcc/keppel/pkg/database"
)

//ServiceUser wraps all the operations that Keppel needs to execute using its
//service user account in OpenStack.
type ServiceUser struct {
	IdentityV3 *gophercloud.ServiceClient

	//The local role is the Keystone role that enables read-write access to a project's Swift account when assigned at the project level. (Its name is given in the KEPPEL_LOCAL_ROLE environment variable.)
	localRoleID string
	//The user ID of Keppel's service user. We need to know this because creating
	//an account creates a role assignment for this service user.
	serviceUserID string
}

//NewServiceUser creates a new ServiceUser instance.
func NewServiceUser(provider *gophercloud.ProviderClient, userID, localRoleName string) (*ServiceUser, error) {
	identityV3, err := openstack.NewIdentityV3(provider, gophercloud.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("cannot find Identity v3 API in Keystone catalog: %s", err.Error())
	}

	localRole, err := getRoleByName(identityV3, localRoleName)
	if err != nil {
		return nil, fmt.Errorf("cannot find Keystone role '%s': %s", localRoleName, err.Error())
	}

	return &ServiceUser{
		IdentityV3:    identityV3,
		localRoleID:   localRole.ID,
		serviceUserID: userID,
	}, nil
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

//AddLocalRole adds a role assignment of KEPPEL_LOCAL_ROLE for the Keppel
//service user in the given project.
func (su *ServiceUser) AddLocalRole(projectUUID string, requestingUser *gopherpolicy.Token) error {
	client, err := openstack.NewIdentityV3(requestingUser.ProviderClient, gophercloud.EndpointOpts{})
	if err != nil {
		return err
	}
	result := roles.Assign(client, su.localRoleID, roles.AssignOpts{
		UserID:    su.serviceUserID,
		ProjectID: projectUUID,
	})
	return result.Err
}

//UserAccess describes the access permissions of a user in a certain scope
//(project or global).
type UserAccess struct {
	Roles []string
}

//CheckUserAccess checks whether the given user has access to the project
//corresponding to the given account (or, for account == nil, just whether the
//user exists and the correct password was given).
//The user name must be given as "username@userdomainname".
//
//Returns (access, nil) if the user exists and has access to the account.
//
//Returns (nil, nil) if the user exists, but does not have access (or no
//account was specified).
//
//Returns (nil, err) in case of authentication failure (e.g. wrong
//username/password) or other failures (e.g. Keystone API down).
func (su *ServiceUser) CheckUserAccess(username, password string, account *database.Account) (*UserAccess, error) {
	usernameFields := strings.SplitN(username, "@", 2)
	if len(usernameFields) != 2 {
		return nil, errors.New(`invalid username (expected "user@domain" format)`)
	}

	authOpts := gophercloud.AuthOptions{
		IdentityEndpoint: su.IdentityV3.Endpoint,
		Username:         usernameFields[0],
		DomainName:       usernameFields[1],
		Password:         password,
	}
	if account != nil {
		authOpts.Scope = &gophercloud.AuthScope{ProjectID: account.ProjectUUID}
	}

	roles, err := tokens.Create(su.IdentityV3, &authOpts).ExtractRoles()
	if err != nil {
		return nil, err
	}

	if account == nil {
		return nil, nil
	}
	roleNames := make([]string, len(roles))
	for idx, role := range roles {
		roleNames[idx] = role.Name
	}
	return &UserAccess{Roles: roleNames}, nil
}
