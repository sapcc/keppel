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

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/roles"
	"github.com/sapcc/go-bits/gopherpolicy"
)

//ServiceUser wraps all the operations that Keppel needs to execute using its
//service user account in OpenStack.
type ServiceUser struct {
	identityV3     *gophercloud.ServiceClient
	tokenValidator *gopherpolicy.TokenValidator

	//The local role is the Keystone role that enables read-write access to a
	//project's Swift account when assigned at the project level.
	localRoleID string
	//The user ID of Keppel's service user. We need to know this because creating
	//an account creates a role assignment for this service user.
	serviceUserID string
}

//NewServiceUser creates a new ServiceUser instance.
func NewServiceUser(provider *gophercloud.ProviderClient, userID, localRoleName, policyFilePath string) (*ServiceUser, error) {
	identityV3, err := openstack.NewIdentityV3(provider, gophercloud.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("cannot find Identity v3 API in Keystone catalog: %s", err.Error())
	}

	tv := gopherpolicy.TokenValidator{
		IdentityV3: identityV3,
	}
	err = tv.LoadPolicyFile(policyFilePath)
	if err != nil {
		return nil, err
	}

	localRole, err := getRoleByName(identityV3, localRoleName)
	if err != nil {
		return nil, fmt.Errorf("cannot find Keystone role '%s': %s", localRoleName, err.Error())
	}

	return &ServiceUser{
		identityV3:     identityV3,
		tokenValidator: &tv,
		localRoleID:    localRole.ID,
		serviceUserID:  userID,
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
//service user in the given project. The `requester` must have been generated
//by GetAccessLevelForRequest().
func (su *ServiceUser) AddLocalRole(projectUUID string, requester AccessLevel) error {
	requesterToken := requester.(token).Token
	client, err := openstack.NewIdentityV3(requesterToken.ProviderClient, gophercloud.EndpointOpts{})
	if err != nil {
		return err
	}
	result := roles.Assign(client, su.localRoleID, roles.AssignOpts{
		UserID:    su.serviceUserID,
		ProjectID: projectUUID,
	})
	return result.Err
}
