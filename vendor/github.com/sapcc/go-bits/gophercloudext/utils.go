// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

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
