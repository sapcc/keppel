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
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/majewsky/schwift"
	"github.com/majewsky/schwift/gopherschwift"
	"github.com/sapcc/keppel/internal/keppel"
)

type swiftDriver struct {
	auth            *keystoneDriver
	cfg             keppel.Configuration
	password        string
	accounts        map[string]*schwift.Account
	containerExists map[string]bool
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
		return &swiftDriver{
			k, cfg, password,
			make(map[string]*schwift.Account),
			make(map[string]bool),
		}, nil
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

func (d *swiftDriver) getBackendConnection(account keppel.Account) (*schwift.Container, error) {
	a, ok := d.accounts[account.AuthTenantID]

	if !ok {
		ao := gophercloud.AuthOptions{
			IdentityEndpoint: d.auth.IdentityV3.Endpoint,
			Username:         d.auth.ServiceUser.Name,
			DomainName:       d.auth.ServiceUser.Domain.Name,
			Password:         d.password,
			AllowReauth:      true,
			Scope:            &gophercloud.AuthScope{ProjectID: account.AuthTenantID},
		}
		provider, err := openstack.AuthenticatedClient(ao)
		if err != nil {
			return nil, err
		}
		client, err := openstack.NewObjectStorageV1(provider, gophercloud.EndpointOpts{})
		if err != nil {
			return nil, err
		}
		a, err := gopherschwift.Wrap(client, &gopherschwift.Options{
			UserAgent: "keppel-api/" + keppel.Version,
		})
		if err != nil {
			return nil, err
		}
		d.accounts[account.AuthTenantID] = a
	}

	c := a.Container(account.SwiftContainerName())
	if d.containerExists[account.Name] {
		return c, nil
	}
	_, err := c.EnsureExists()
	if err == nil {
		d.containerExists[account.Name] = true
	}
	return c, err
}

//ReadManifest implements the keppel.StorageDriver interface.
func (d *swiftDriver) ReadManifest(account keppel.Account, repoName, digest string) ([]byte, error) {
	c, err := d.getBackendConnection(account)
	if err != nil {
		return nil, err
	}
	o := manifestObject(c, repoName, digest)
	return o.Download(nil).AsByteSlice()
}

//WriteManifest implements the keppel.StorageDriver interface.
func (d *swiftDriver) WriteManifest(account keppel.Account, repoName, digest string, contents []byte) error {
	c, err := d.getBackendConnection(account)
	if err != nil {
		return err
	}
	o := manifestObject(c, repoName, digest)
	return o.Upload(bytes.NewReader(contents), nil, nil)
}

//DeleteManifest implements the keppel.StorageDriver interface.
func (d *swiftDriver) DeleteManifest(account keppel.Account, repoName, digest string) error {
	c, err := d.getBackendConnection(account)
	if err != nil {
		return err
	}
	o := manifestObject(c, repoName, digest)
	return o.Delete(nil, nil)
}

func manifestObject(c *schwift.Container, repoName, digest string) *schwift.Object {
	return c.Object(fmt.Sprintf("manifests/%s/%s", repoName, digest))
}
