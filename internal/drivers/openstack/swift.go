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
	"os"

	"github.com/sapcc/keppel/internal/keppel"
)

type swiftDriver struct {
	auth *keystoneDriver
}

func init() {
	keppel.RegisterStorageDriver("swift", func(driver keppel.AuthDriver) (keppel.StorageDriver, error) {
		k, ok := driver.(*keystoneDriver)
		if !ok {
			return nil, keppel.ErrAuthDriverMismatch
		}
		return &swiftDriver{k}, nil
	})
}

//GetEnvironment implements the keppel.StorageDriver interface.
func (d *swiftDriver) GetEnvironment(account keppel.Account) ([]string, error) {
	//cf. cmd/keppel-api/main.go
	insecure := "false"
	if os.Getenv("KEPPEL_INSECURE") == "1" {
		insecure = "true"
	}

	password := os.Getenv("OS_PASSWORD")
	if password == "" {
		return nil, errors.New("missing environment variable: OS_PASSWORD")
	}

	return []string{
		"REGISTRY_STORAGE_SWIFT-PLUS_AUTHURL=" + d.auth.IdentityV3.Endpoint,
		"REGISTRY_STORAGE_SWIFT-PLUS_USERID=" + d.auth.ServiceUserID,
		"REGISTRY_STORAGE_SWIFT-PLUS_PASSWORD=" + password,
		"REGISTRY_STORAGE_SWIFT-PLUS_PROJECTID=" + account.AuthTenantID,
		"REGISTRY_STORAGE_SWIFT-PLUS_CONTAINER=" + account.SwiftContainerName(),
		"REGISTRY_STORAGE_SWIFT-PLUS_POSTGRESURI=" + account.PostgresDatabaseName(),
		"REGISTRY_STORAGE_SWIFT-PLUS_INSECURESKIPVERIFY=" + insecure,
	}, nil
}
