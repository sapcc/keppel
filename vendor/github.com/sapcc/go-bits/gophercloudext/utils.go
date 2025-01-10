/*******************************************************************************
*
* Copyright 2024 SAP SE
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

package gophercloudext

import (
	"fmt"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
)

// GetProjectIDFromTokenScope returns the project ID from the client's token scope.
//
// This is useful in applications that usually operate on the cloud-admin level,
// when using an API endpoint that requires a project ID in its URL.
// Usually this is then overridden by a query parameter like "?all_projects=True".
func GetProjectIDFromTokenScope(provider *gophercloud.ProviderClient) (string, error) {
	result, ok := provider.GetAuthResult().(tokens.CreateResult)
	if !ok {
		return "", fmt.Errorf("%T is not a %T", provider.GetAuthResult(), tokens.CreateResult{})
	}
	project, err := result.ExtractProject()
	if err != nil {
		return "", err
	}
	if project == nil || project.ID == "" {
		return "", fmt.Errorf(`expected "id" attribute in "project" section, but got %#v`, project)
	}
	return project.ID, nil
}
