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
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/majewsky/schwift"
	"github.com/majewsky/schwift/gopherschwift"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/keppel"
)

type swiftContainerInfo struct {
	TempURLKey string
	CachedAt   time.Time
}

type swiftDriver struct {
	mainAccount         *schwift.Account
	containerInfos      map[string]*swiftContainerInfo
	containerInfosMutex sync.RWMutex
}

func init() {
	keppel.RegisterStorageDriver("swift", func(driver keppel.AuthDriver, cfg keppel.Configuration) (keppel.StorageDriver, error) {
		k, ok := driver.(*keystoneDriver)
		if !ok {
			return nil, keppel.ErrAuthDriverMismatch
		}

		eo := gophercloud.EndpointOpts{
			//note that empty values are acceptable in both fields
			Region:       os.Getenv("OS_REGION_NAME"),
			Availability: gophercloud.Availability(os.Getenv("OS_INTERFACE")),
		}
		client, err := openstack.NewObjectStorageV1(k.Provider, eo)
		if err != nil {
			return nil, err
		}

		swiftAccount, err := gopherschwift.Wrap(client, &gopherschwift.Options{
			UserAgent: fmt.Sprintf("%s/%s", keppel.Component, keppel.Version),
		})
		if err != nil {
			return nil, err
		}

		return &swiftDriver{
			mainAccount:    swiftAccount,
			containerInfos: make(map[string]*swiftContainerInfo),
		}, nil
	})
}

//TODO translate errors from Swift into keppel.RegistryV2Error where
//appropriate (esp. keppel.ErrSizeInvalid and keppel.ErrTooManyRequests)

func (d *swiftDriver) getBackendConnection(account keppel.Account) (*schwift.Container, *swiftContainerInfo, error) {
	c := d.mainAccount.SwitchAccount("AUTH_" + account.AuthTenantID).Container(account.SwiftContainerName())

	//we want to cache the tempurl key to speed up URLForBlob() calls; but we
	//cannot cache it indefinitely because the Keppel account (and hence the
	//Swift container) may get deleted and later re-created with a different
	//tempurl key; by only caching tempurl keys for a few minutes, we get the
	//opportunity to self-heal when the tempurl key changes
	d.containerInfosMutex.RLock()
	info := d.containerInfos[account.Name]
	d.containerInfosMutex.RUnlock()

	if info == nil || time.Since(info.CachedAt) > 5*time.Minute {
		//this also ensures that the container exists
		_, err := c.EnsureExists()
		if err != nil {
			return nil, nil, err
		}
		hdr, err := c.Headers()
		if err != nil {
			return nil, nil, err
		}

		tempURLKey := hdr.TempURLKey().Get()
		if tempURLKey == "" {
			tempURLKey = hdr.TempURLKey2().Get()
		}
		if tempURLKey == "" {
			//generate tempurl key on first startup
			tempURLKey, err = generateSecret()
			if err != nil {
				return nil, nil, err
			}
			hdr := schwift.NewContainerHeaders()
			hdr.TempURLKey().Set(tempURLKey)
			err = c.Update(hdr, nil)
			if err != nil {
				return nil, nil, err
			}
		}

		info = &swiftContainerInfo{
			TempURLKey: tempURLKey,
			CachedAt:   time.Now(),
		}
		d.containerInfosMutex.Lock()
		d.containerInfos[account.Name] = info
		d.containerInfosMutex.Unlock()
	}

	return c, info, nil
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

//Like schwift.Object.Upload(), but does a HEAD request on the object
//beforehand to ensure that we have a valid token. There seems to be a problem
//in gopherschwift with restarting requests with request bodies after
//reauthentication, even though [1] is supposed to handle this case.
//
//[1] https://github.com/majewsky/schwift/blob/3857990bb9f705ed06f8ac2a18ba7d4a732f4274/gopherschwift/package.go#L124-L135
func uploadToObject(o *schwift.Object, content io.Reader, opts *schwift.UploadOptions, ropts *schwift.RequestOptions) error {
	_, err := o.Headers()
	if err != nil && !schwift.Is(err, http.StatusNotFound) {
		return err
	}
	return o.Upload(content, opts, ropts)
}

//AppendToBlob implements the keppel.StorageDriver interface.
func (d *swiftDriver) AppendToBlob(account keppel.Account, storageID string, chunkNumber uint32, chunkLength *uint64, chunk io.Reader) error {
	c, _, err := d.getBackendConnection(account)
	if err != nil {
		return err
	}
	hdr := schwift.NewObjectHeaders()
	if chunkLength != nil {
		hdr.SizeBytes().Set(*chunkLength)
	}
	o := chunkObject(c, storageID, chunkNumber)
	return uploadToObject(o, chunk, nil, hdr.ToOpts())
}

//FinalizeBlob implements the keppel.StorageDriver interface.
func (d *swiftDriver) FinalizeBlob(account keppel.Account, storageID string, chunkCount uint32) error {
	c, _, err := d.getBackendConnection(account)
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
	c, _, err := d.getBackendConnection(account)
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
	c, _, err := d.getBackendConnection(account)
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
	c, info, err := d.getBackendConnection(account)
	if err != nil {
		return "", err
	}

	expiresAt := time.Now().Add(20 * time.Minute)
	return blobObject(c, storageID).TempURL(info.TempURLKey, "GET", expiresAt)
}

//DeleteBlob implements the keppel.StorageDriver interface.
func (d *swiftDriver) DeleteBlob(account keppel.Account, storageID string) error {
	c, _, err := d.getBackendConnection(account)
	if err != nil {
		return err
	}
	err = blobObject(c, storageID).Delete(&schwift.DeleteOptions{DeleteSegments: true}, nil)
	reportObjectErrorsIfAny("DeleteBlob", err)
	return err
}

func reportObjectErrorsIfAny(operation string, err error) {
	if berr, ok := err.(schwift.BulkError); ok {
		//When we return this `err` to the Keppel core, it will only look at
		//Error() which prints only the summary (e.g. "400 Bad Request (+2 object
		//errors)"). This method ensures that the individual object errors also
		//get logged.
		for _, oerr := range berr.ObjectErrors {
			logg.Error("encountered error during Swift bulk operation for object %s", oerr.Error())
		}
	}
}

//ReadManifest implements the keppel.StorageDriver interface.
func (d *swiftDriver) ReadManifest(account keppel.Account, repoName, digest string) ([]byte, error) {
	c, _, err := d.getBackendConnection(account)
	if err != nil {
		return nil, err
	}
	o := manifestObject(c, repoName, digest)
	return o.Download(nil).AsByteSlice()
}

//WriteManifest implements the keppel.StorageDriver interface.
func (d *swiftDriver) WriteManifest(account keppel.Account, repoName, digest string, contents []byte) error {
	c, _, err := d.getBackendConnection(account)
	if err != nil {
		return err
	}
	o := manifestObject(c, repoName, digest)
	return uploadToObject(o, bytes.NewReader(contents), nil, nil)
}

//DeleteManifest implements the keppel.StorageDriver interface.
func (d *swiftDriver) DeleteManifest(account keppel.Account, repoName, digest string) error {
	c, _, err := d.getBackendConnection(account)
	if err != nil {
		return err
	}
	o := manifestObject(c, repoName, digest)
	return o.Delete(nil, nil)
}

var (
	//These regexes are used to reconstruct the storage ID from a blob's or chunk's object name.
	//It's kinda the reverse of func blobObject() or func checkObject().
	blobObjectNameRx  = regexp.MustCompile(`^_blobs/([^/]{2})/([^/]{2})/([^/]+)$`)
	chunkObjectNameRx = regexp.MustCompile(`^_chunks/([^/]{2})/([^/]{2})/([^/]+)/([0-9]+)$`)
	//This regex recovers the repo name and manifest digest from a manifest's object name.
	//It's kinda the reverse of func manifestObject().
	manifestObjectNameRx = regexp.MustCompile(`^(.+)/_manifests/([^/]+)$`)
)

//ListStorageContents implements the keppel.StorageDriver interface.
func (d *swiftDriver) ListStorageContents(account keppel.Account) ([]keppel.StoredBlobInfo, []keppel.StoredManifestInfo, error) {
	c, _, err := d.getBackendConnection(account)
	if err != nil {
		return nil, nil, err
	}

	chunkCounts := make(map[string]uint32) //key = storage ID, value = same semantics as keppel.StoredBlobInfo.ChunkCount
	var manifests []keppel.StoredManifestInfo

	err = c.Objects().Foreach(func(o *schwift.Object) error {
		if match := blobObjectNameRx.FindStringSubmatch(o.Name()); match != nil {
			storageID := match[1] + match[2] + match[3]
			mergeChunkCount(chunkCounts, storageID, 0)
			return nil
		}
		if match := chunkObjectNameRx.FindStringSubmatch(o.Name()); match != nil {
			storageID := match[1] + match[2] + match[3]
			chunkNumber, err := strconv.ParseUint(match[4], 10, 32)
			if err != nil {
				return fmt.Errorf("while parsing chunk object name %s: %s", o.Name(), err.Error())
			}
			mergeChunkCount(chunkCounts, storageID, uint32(chunkNumber))
			return nil
		}
		if match := manifestObjectNameRx.FindStringSubmatch(o.Name()); match != nil {
			manifests = append(manifests, keppel.StoredManifestInfo{
				RepoName: match[1],
				Digest:   match[2],
			})
			return nil
		}
		return fmt.Errorf("encountered unexpected object while listing storage contents of account %s: %s", account.Name, o.Name())
	})
	if err != nil {
		return nil, nil, err
	}

	blobs := make([]keppel.StoredBlobInfo, 0, len(chunkCounts))
	for storageID, chunkCount := range chunkCounts {
		blobs = append(blobs, keppel.StoredBlobInfo{
			StorageID:  storageID,
			ChunkCount: chunkCount,
		})
	}

	return blobs, manifests, nil
}

//See comment on keppel.StoredBlobInfo.ChunkCount for explanation of semantics.
func mergeChunkCount(chunkCounts map[string]uint32, key string, chunkNumber uint32) {
	prevCount, exists := chunkCounts[key]
	if !exists {
		//nothing to merge, just record the new value
		chunkCounts[key] = chunkNumber
		return
	}

	//The value 0 indicates a finalized blob and therefore takes precedence over actual chunk numbers.
	if prevCount == 0 || chunkNumber == 0 {
		chunkCounts[key] = 0
		return
	}
	//If 0 is not involved, remember the largest chunk number as that's gonna be the chunk count.
	if chunkNumber > prevCount {
		chunkCounts[key] = chunkNumber
	}
}

//CleanupAccount implements the keppel.StorageDriver interface.
func (d *swiftDriver) CleanupAccount(account keppel.Account) error {
	c, _, err := d.getBackendConnection(account)
	if err != nil {
		return err
	}
	return c.Delete(nil)
}
