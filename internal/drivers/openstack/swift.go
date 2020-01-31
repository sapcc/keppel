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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/majewsky/schwift"
	"github.com/majewsky/schwift/gopherschwift"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/keppel"
)

type swiftDriver struct {
	auth                 *keystoneDriver
	cfg                  keppel.Configuration
	password             string
	accounts             map[string]*schwift.Account
	containerTempURLKeys map[string]string
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
			make(map[string]string),
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

	//on first use, cache the tempurl key (for fast read access to blobs)
	if d.containerTempURLKeys[account.Name] == "" {
		//this also ensures that the container exists
		_, err := c.EnsureExists()
		if err != nil {
			return nil, err
		}
		hdr, err := c.Headers()
		if err != nil {
			return nil, err
		}

		tempURLKey := hdr.TempURLKey().Get()
		if tempURLKey == "" {
			tempURLKey = hdr.TempURLKey2().Get()
		}
		if tempURLKey == "" {
			//generate tempurl key on first startup
			tempURLKey, err = generateSecret()
			if err != nil {
				return nil, err
			}
			hdr := schwift.NewContainerHeaders()
			hdr.TempURLKey().Set(tempURLKey)
			err = c.Update(hdr, nil)
			if err != nil {
				return nil, err
			}
		}
		d.containerTempURLKeys[account.Name] = tempURLKey
	}

	return c, nil
}

func generateSecret() (string, error) {
	var secretBytes [32]byte
	if _, err := rand.Read(secretBytes[:]); err != nil {
		return "", fmt.Errorf("could not generate random bytes for Swift secret key: %v", err)
	}
	return hex.EncodeToString(secretBytes[:]), nil
}

func blobObject(c *schwift.Container, storageID string) *schwift.Object {
	return c.Object(fmt.Sprintf("_blobs/%s/%s/%s", storageID[0:2], storageID[2:4], storageID[4:]))
}

func chunkObject(c *schwift.Container, storageID string, chunkNumber uint32) *schwift.Object {
	//NOTE: uint32 numbers never have more than 10 digits
	return c.Object(fmt.Sprintf("_chunks/%s/%s/%s/%010d", storageID[0:2], storageID[2:4], storageID[4:], chunkNumber))
}

func manifestObject(c *schwift.Container, repoName, digest string) *schwift.Object {
	return c.Object(fmt.Sprintf("%s/_manifests/%s", repoName, digest))
}

//AppendToBlob implements the keppel.StorageDriver interface.
func (d *swiftDriver) AppendToBlob(account keppel.Account, storageID string, chunkNumber uint32, chunkLength *uint64, chunk io.Reader) error {
	c, err := d.getBackendConnection(account)
	if err != nil {
		return err
	}
	hdr := schwift.NewObjectHeaders()
	if chunkLength != nil {
		hdr.SizeBytes().Set(*chunkLength)
	}
	return chunkObject(c, storageID, chunkNumber).Upload(chunk, nil, hdr.ToOpts())
}

//FinalizeBlob implements the keppel.StorageDriver interface.
func (d *swiftDriver) FinalizeBlob(account keppel.Account, storageID string, chunkCount uint32) error {
	c, err := d.getBackendConnection(account)
	if err != nil {
		return err
	}
	lo, err := blobObject(c, storageID).AsNewLargeObject(
		schwift.SegmentingOptions{
			Strategy:         schwift.StaticLargeObject,
			SegmentContainer: c, //ignored since we AddSegment() manually
		},
		&schwift.TruncateOptions{DeleteSegments: false},
	)
	if err != nil {
		return err
	}

	for chunkNumber := uint32(1); chunkNumber <= chunkCount; chunkNumber++ {
		co := chunkObject(c, storageID, chunkNumber)
		hdr, err := co.Headers()
		if err != nil {
			return err
		}
		err = lo.AddSegment(schwift.SegmentInfo{
			Object:    co,
			SizeBytes: hdr.SizeBytes().Get(),
			Etag:      hdr.Etag().Get(),
		})
		if err != nil {
			return err
		}
	}

	return lo.WriteManifest(nil)
}

//AbortBlobUpload implements the keppel.StorageDriver interface.
func (d *swiftDriver) AbortBlobUpload(account keppel.Account, storageID string, chunkCount uint32) error {
	c, err := d.getBackendConnection(account)
	if err != nil {
		return err
	}

	//we didn't construct the LargeObject yet, so we need to delete the segments individually
	var firstError error
	for chunkNumber := uint32(1); chunkNumber <= chunkCount; chunkNumber++ {
		err := chunkObject(c, storageID, chunkNumber).Delete(nil, nil)
		//keep going even when some segments cannot be deleted, to clean up as much as we can
		if err != nil {
			if firstError == nil {
				firstError = err
			} else {
				logg.Error("encountered additional error while cleaning up segments of %s: %s",
					chunkObject(c, storageID, chunkNumber).FullName(), err.Error(),
				)
			}
		}
	}

	return firstError
}

//ReadBlob implements the keppel.StorageDriver interface.
func (d *swiftDriver) ReadBlob(account keppel.Account, storageID string) (io.ReadCloser, uint64, error) {
	c, err := d.getBackendConnection(account)
	if err != nil {
		return nil, 0, err
	}
	o := blobObject(c, storageID)
	hdr, err := o.Headers()
	if err != nil {
		return nil, 0, err
	}
	reader, err := o.Download(nil).AsReadCloser()
	return reader, hdr.SizeBytes().Get(), err
}

//URLForBlob implements the keppel.StorageDriver interface.
func (d *swiftDriver) URLForBlob(account keppel.Account, storageID string) (string, error) {
	c, err := d.getBackendConnection(account)
	if err != nil {
		return "", err
	}

	return blobObject(c, storageID).TempURL(
		d.containerTempURLKeys[account.Name], //this was filled in getBackendConnection()
		"GET",
		time.Now().Add(20*time.Minute), //expiry date
	)
}

//DeleteBlob implements the keppel.StorageDriver interface.
func (d *swiftDriver) DeleteBlob(account keppel.Account, storageID string) error {
	c, err := d.getBackendConnection(account)
	if err != nil {
		return err
	}
	return blobObject(c, storageID).Delete(&schwift.DeleteOptions{DeleteSegments: true}, nil)
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
