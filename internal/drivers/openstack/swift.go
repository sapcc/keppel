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
	"strconv"

	"github.com/sapcc/keppel/internal/keppel"
)

type swiftDriver struct {
	auth     *keystoneDriver
	cfg      keppel.Configuration
	password string
}

func init() {
	keppel.RegisterStorageDriver("swift", func(driver keppel.AuthDriver, cfg keppel.Configuration) (keppel.StorageDriver, error) {
		k, ok := driver.(*keystoneDriver)
		if !ok {
			return nil, keppel.ErrAuthDriverMismatch
		}
		password := os.Getenv("OS_PASSWORD")
		if password == "" {
			return nil, errors.New("missing environment variable: OS_PASSWORD")
		}
		return &swiftDriver{k, cfg, password}, nil
	})
}

//GetEnvironment implements the keppel.StorageDriver interface.
func (d *swiftDriver) GetEnvironment(account keppel.Account) map[string]string {
	//cf. cmd/keppel-api/main.go
	insecure, err := strconv.ParseBool(os.Getenv("KEPPEL_INSECURE"))
	if err != nil {
		insecure = false
	}

	postgresURL := d.cfg.DatabaseURL
	postgresURL.Path = "/" + account.PostgresDatabaseName()

	return map[string]string{
		"REGISTRY_STORAGE_SWIFT-PLUS_POSTGRESURI":        postgresURL.String(),
		"REGISTRY_STORAGE_SWIFT-PLUS_AUTHURL":            d.auth.IdentityV3.Endpoint,
		"REGISTRY_STORAGE_SWIFT-PLUS_USERNAME":           d.auth.ServiceUser.Name,
		"REGISTRY_STORAGE_SWIFT-PLUS_USERDOMAINNAME":     d.auth.ServiceUser.Domain.Name,
		"REGISTRY_STORAGE_SWIFT-PLUS_PASSWORD":           d.password,
		"REGISTRY_STORAGE_SWIFT-PLUS_PROJECTID":          account.AuthTenantID,
		"REGISTRY_STORAGE_SWIFT-PLUS_CONTAINER":          account.SwiftContainerName(),
		"REGISTRY_STORAGE_SWIFT-PLUS_INSECURESKIPVERIFY": strconv.FormatBool(insecure),
	}
}
