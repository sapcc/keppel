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
	"os"

	"github.com/sapcc/keppel/pkg/database"
	"github.com/sapcc/keppel/pkg/keppel"
)

type swiftDriver struct {
}

func init() {
	keppel.RegisterStorageDriver("swift", func() keppel.StorageDriver {
		return &swiftDriver{}
	})
}

//ReadConfig implements the keppel.StorageDriver interface.
func (d *swiftDriver) ReadConfig(unmarshal func(interface{}) error) error {
	//this driver does not have any config options
	return nil
}

//GetEnvironment implements the keppel.StorageDriver interface.
func (d *swiftDriver) GetEnvironment(account database.Account, driver keppel.AuthDriver) ([]string, error) {
	k, ok := driver.(*keystoneDriver)
	if !ok {
		return nil, keppel.ErrAuthDriverMismatch
	}

	//cf. cmd/keppel-api/main.go
	insecure := "false"
	if os.Getenv("KEPPEL_INSECURE") == "1" {
		insecure = "true"
	}

	return []string{
		"REGISTRY_STORAGE_SWIFT-PLUS_AUTHURL=" + k.ServiceUser.AuthURL,
		"REGISTRY_STORAGE_SWIFT-PLUS_USERNAME=" + k.ServiceUser.UserName,
		"REGISTRY_STORAGE_SWIFT-PLUS_USERDOMAINNAME=" + k.ServiceUser.UserDomainName,
		"REGISTRY_STORAGE_SWIFT-PLUS_PASSWORD=" + k.ServiceUser.Password,
		"REGISTRY_STORAGE_SWIFT-PLUS_PROJECTID=" + account.AuthTenantID,
		"REGISTRY_STORAGE_SWIFT-PLUS_CONTAINER=" + account.SwiftContainerName(),
		"REGISTRY_STORAGE_SWIFT-PLUS_POSTGRESURI=" + account.PostgresDatabaseName(),
		"REGISTRY_STORAGE_SWIFT-PLUS_INSECURESKIPVERIFY=" + insecure,
	}, nil
}
