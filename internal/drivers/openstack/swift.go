// SPDX-FileCopyrightText: 2018 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package openstack

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gophercloud/gophercloud/v2/openstack"
	. "github.com/majewsky/gg/option"
	"github.com/majewsky/schwift/v2"
	"github.com/majewsky/schwift/v2/gopherschwift"
	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/errext"
	"github.com/sapcc/go-bits/logg"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/stringy"
	"github.com/sapcc/keppel/internal/trivy"
)

type swiftContainerInfo struct {
	TempURLKey string
	CachedAt   time.Time
}

type swiftDriver struct {
	mainAccount         *schwift.Account
	containerInfos      map[models.AccountName]*swiftContainerInfo
	containerInfosMutex sync.RWMutex
}

func init() {
	keppel.StorageDriverRegistry.Add(func() keppel.StorageDriver { return &swiftDriver{} })
}

// PluginTypeID implements the keppel.StorageDriver interface.
func (d *swiftDriver) PluginTypeID() string { return "swift" }

// Init implements the keppel.StorageDriver interface.
func (d *swiftDriver) Init(ad keppel.AuthDriver, cfg keppel.Configuration) error {
	k, ok := ad.(*keystoneDriver)
	if !ok {
		return keppel.ErrAuthDriverMismatch
	}

	client, err := openstack.NewObjectStorageV1(k.Provider, k.EndpointOpts)
	if err != nil {
		return err
	}

	d.mainAccount, err = gopherschwift.Wrap(client, nil)
	if err != nil {
		return err
	}
	d.containerInfos = make(map[models.AccountName]*swiftContainerInfo)
	return nil
}

// TODO translate errors from Swift into keppel.RegistryV2Error where
// appropriate (esp. keppel.ErrSizeInvalid and keppel.ErrTooManyRequests)

func (d *swiftDriver) getBackendAccount(account models.ReducedAccount) *schwift.Account {
	return d.mainAccount.SwitchAccount("AUTH_" + account.AuthTenantID)
}

func (d *swiftDriver) getBackendConnection(ctx context.Context, account models.ReducedAccount) (*schwift.Container, *swiftContainerInfo, error) {
	containerName := "keppel-" + string(account.Name)
	c := d.getBackendAccount(account).Container(containerName)

	// we want to cache the tempurl key to speed up URLForBlob() calls; but we
	// cannot cache it indefinitely because the Keppel account (and hence the
	// Swift container) may get deleted and later re-created with a different
	// tempurl key; by only caching tempurl keys for a few minutes, we get the
	// opportunity to self-heal when the tempurl key changes
	d.containerInfosMutex.RLock()
	info := d.containerInfos[account.Name]
	d.containerInfosMutex.RUnlock()

	if info == nil || time.Since(info.CachedAt) > 5*time.Minute {
		// get container metadata (404 is not a problem, in that case we will create the container down below)
		hdr, err := c.Headers(ctx)
		if err != nil && !schwift.Is(err, http.StatusNotFound) {
			return nil, nil, err
		}

		tempURLKey := hdr.TempURLKey().Get()
		if tempURLKey == "" {
			tempURLKey = hdr.TempURLKey2().Get()
		}
		//nolint:errcheck //in case of error, False will be returned therefore no need to check.
		writeRestricted, _ := strconv.ParseBool(hdr.Metadata().Get("Write-Restricted"))
		if tempURLKey == "" || !writeRestricted {
			hdr := schwift.NewContainerHeaders()
			// generate tempurl key on first startup
			if tempURLKey == "" {
				tempURLKey, err = generateSecret()
				if err != nil {
					return nil, nil, err
				}
				hdr.TempURLKey().Set(tempURLKey)
			}
			// add the 'X-Container-Meta-Write-Restricted' metadata header to restrict writes only
			// to allowed users. See: https://github.com/sapcc/swift-addons/tree/master#write-restriction
			if !writeRestricted {
				hdr.Metadata().Set("Write-Restricted", "true")
			}
			err = c.Create(ctx, hdr.ToOpts())
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
		return "", fmt.Errorf("could not generate random bytes for Swift secret key: %w", err)
	}
	return hex.EncodeToString(secretBytes[:]), nil
}

// Like schwift.Object.Upload(), but does a HEAD request on the object
// beforehand to ensure that we have a valid token. There seems to be a problem
// in gopherschwift with restarting requests with request bodies after
// reauthentication, even though [1] is supposed to handle this case.
//
// [1] https://github.com/majewsky/schwift/blob/3857990bb9f705ed06f8ac2a18ba7d4a732f4274/gopherschwift/package.go#L124-L135
func uploadToObject(ctx context.Context, o *schwift.Object, content io.Reader, opts *schwift.UploadOptions, ropts *schwift.RequestOptions) error {
	_, err := o.Headers(ctx)
	if err != nil && !schwift.Is(err, http.StatusNotFound) {
		return err
	}
	return o.Upload(ctx, content, opts, ropts)
}

// AppendToBlob implements the keppel.StorageDriver interface.
func (d *swiftDriver) AppendToBlob(ctx context.Context, account models.ReducedAccount, storageID string, chunkNumber uint32, chunkLength Option[uint64], chunk io.Reader) error {
	c, _, err := d.getBackendConnection(ctx, account)
	if err != nil {
		return err
	}
	hdr := schwift.NewObjectHeaders()
	if l, ok := chunkLength.Unpack(); ok {
		hdr.SizeBytes().Set(l)
	}
	o := c.Object(stringy.ChunkObjectName(storageID, chunkNumber))
	return uploadToObject(ctx, o, chunk, nil, hdr.ToOpts())
}

// FinalizeBlob implements the keppel.StorageDriver interface.
func (d *swiftDriver) FinalizeBlob(ctx context.Context, account models.ReducedAccount, storageID string, chunkCount uint32) error {
	c, _, err := d.getBackendConnection(ctx, account)
	if err != nil {
		return err
	}
	lo, err := c.Object(stringy.BlobObjectName(storageID)).AsNewLargeObject(
		ctx,
		schwift.SegmentingOptions{
			Strategy:         schwift.StaticLargeObject,
			SegmentContainer: c, // ignored since we AddSegment() manually
		},
		&schwift.TruncateOptions{DeleteSegments: false},
	)
	if err != nil {
		return err
	}

	for chunkNumber := uint32(1); chunkNumber <= chunkCount; chunkNumber++ {
		co := c.Object(stringy.ChunkObjectName(storageID, chunkNumber))
		hdr, err := co.Headers(ctx)
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

	return lo.WriteManifest(ctx, nil)
}

// AbortBlobUpload implements the keppel.StorageDriver interface.
func (d *swiftDriver) AbortBlobUpload(ctx context.Context, account models.ReducedAccount, storageID string, chunkCount uint32) error {
	c, _, err := d.getBackendConnection(ctx, account)
	if err != nil {
		return err
	}

	// we didn't construct the LargeObject yet, so we need to delete the segments individually
	var firstError error
	for chunkNumber := uint32(1); chunkNumber <= chunkCount; chunkNumber++ {
		err := c.Object(stringy.ChunkObjectName(storageID, chunkNumber)).Delete(ctx, nil, nil)
		// keep going even when some segments cannot be deleted, to clean up as much as we can
		// (404 errors are ignored entirely; they are not really an error since we want the objects to be not there anyway)
		if err != nil && !schwift.Is(err, http.StatusNotFound) {
			if firstError == nil {
				firstError = err
			} else {
				logg.Error("encountered additional error while cleaning up segments of %s: %s",
					c.Object(stringy.ChunkObjectName(storageID, chunkNumber)).FullName(), err.Error(),
				)
			}
		}
	}

	return firstError
}

// ReadBlob implements the keppel.StorageDriver interface.
func (d *swiftDriver) ReadBlob(ctx context.Context, account models.ReducedAccount, storageID string) (io.ReadCloser, uint64, error) {
	c, _, err := d.getBackendConnection(ctx, account)
	if err != nil {
		return nil, 0, err
	}
	o := c.Object(stringy.BlobObjectName(storageID))
	hdr, err := o.Headers(ctx)
	if err != nil {
		return nil, 0, err
	}
	reader, err := o.Download(ctx, nil).AsReadCloser()
	return reader, hdr.SizeBytes().Get(), err
}

// URLForBlob implements the keppel.StorageDriver interface.
func (d *swiftDriver) URLForBlob(ctx context.Context, account models.ReducedAccount, storageID string) (string, error) {
	c, info, err := d.getBackendConnection(ctx, account)
	if err != nil {
		return "", err
	}

	expiresAt := time.Now().Add(20 * time.Minute)
	return c.Object(stringy.BlobObjectName(storageID)).TempURL(ctx, info.TempURLKey, "GET", expiresAt)
}

// DeleteBlob implements the keppel.StorageDriver interface.
func (d *swiftDriver) DeleteBlob(ctx context.Context, account models.ReducedAccount, storageID string) error {
	c, _, err := d.getBackendConnection(ctx, account)
	if err != nil {
		return err
	}
	err = c.Object(stringy.BlobObjectName(storageID)).Delete(ctx, &schwift.DeleteOptions{DeleteSegments: true}, nil)
	reportObjectErrorsIfAny("DeleteBlob", err)
	return err
}

func reportObjectErrorsIfAny(operation string, err error) {
	if berr, ok := errext.As[schwift.BulkError](err); ok {
		// When we return this `err` to the Keppel core, it will only look at
		// Error() which prints only the summary (e.g. "400 Bad Request (+2 object
		// errors)"). This method ensures that the individual object errors also
		// get logged.
		for _, oerr := range berr.ObjectErrors {
			logg.Error("encountered error during Swift bulk operation %s for object %s", operation, oerr.Error())
		}
	}
}

// ReadManifest implements the keppel.StorageDriver interface.
func (d *swiftDriver) ReadManifest(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest) ([]byte, error) {
	c, _, err := d.getBackendConnection(ctx, account)
	if err != nil {
		return nil, err
	}
	o := c.Object(stringy.ManifestObjectName(repoName, manifestDigest))
	return o.Download(ctx, nil).AsByteSlice()
}

// WriteManifest implements the keppel.StorageDriver interface.
func (d *swiftDriver) WriteManifest(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, contents []byte) error {
	c, _, err := d.getBackendConnection(ctx, account)
	if err != nil {
		return err
	}
	o := c.Object(stringy.ManifestObjectName(repoName, manifestDigest))
	return uploadToObject(ctx, o, bytes.NewReader(contents), nil, nil)
}

// DeleteManifest implements the keppel.StorageDriver interface.
func (d *swiftDriver) DeleteManifest(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest) error {
	c, _, err := d.getBackendConnection(ctx, account)
	if err != nil {
		return err
	}
	o := c.Object(stringy.ManifestObjectName(repoName, manifestDigest))
	return o.Delete(ctx, nil, nil)
}

// ReadTrivyReport implements the keppel.StorageDriver interface.
func (d *swiftDriver) ReadTrivyReport(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, format string) ([]byte, error) {
	c, _, err := d.getBackendConnection(ctx, account)
	if err != nil {
		return nil, err
	}
	o := c.Object(stringy.TrivyReportObjectName(repoName, manifestDigest, format))
	return o.Download(ctx, nil).AsByteSlice()
}

// WriteTrivyReport implements the keppel.StorageDriver interface.
func (d *swiftDriver) WriteTrivyReport(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, payload trivy.ReportPayload) error {
	c, _, err := d.getBackendConnection(ctx, account)
	if err != nil {
		return err
	}
	o := c.Object(stringy.TrivyReportObjectName(repoName, manifestDigest, payload.Format))
	return uploadToObject(ctx, o, bytes.NewReader(payload.Contents), nil, nil)
}

// DeleteTrivyReport implements the keppel.StorageDriver interface.
func (d *swiftDriver) DeleteTrivyReport(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, format string) error {
	c, _, err := d.getBackendConnection(ctx, account)
	if err != nil {
		return err
	}
	o := c.Object(stringy.TrivyReportObjectName(repoName, manifestDigest, format))
	return o.Delete(ctx, nil, nil)
}

// ListStorageContents implements the keppel.StorageDriver interface.
func (d *swiftDriver) ListStorageContents(ctx context.Context, account models.ReducedAccount) ([]keppel.StoredBlobInfo, []keppel.StoredManifestInfo, []keppel.StoredTrivyReportInfo, error) {
	c, _, err := d.getBackendConnection(ctx, account)
	if err != nil {
		return nil, nil, nil, err
	}

	chunkCounts := make(map[string]uint32) // key = storage ID, value = same semantics as keppel.StoredBlobInfo.ChunkCount
	var (
		manifests []keppel.StoredManifestInfo
		reports   []keppel.StoredTrivyReportInfo
	)

	err = c.Objects().Foreach(ctx, func(o *schwift.Object) error {
		if storageID := stringy.ParseBlobObjectName(o.Name()); storageID != "" {
			mergeChunkCount(chunkCounts, storageID, 0)
			return nil
		}
		if storageID, chunkNumber, err := stringy.ParseChunkObjectName(o.Name()); err != nil || storageID != "" {
			if err != nil {
				return fmt.Errorf("while parsing chunk object name %q: %w", o.Name(), err)
			}
			mergeChunkCount(chunkCounts, storageID, chunkNumber)
			return nil
		}
		if repoName, manifestDigest, err := stringy.ParseManifestObjectName(o.Name()); err != nil || repoName != "" {
			if err != nil {
				return err
			}
			manifests = append(manifests, keppel.StoredManifestInfo{
				RepositoryName: repoName,
				Digest:         manifestDigest,
			})
			return nil
		}
		if repoName, manifestDigest, format, err := stringy.ParseTrivyReportObjectName(o.Name()); err != nil || repoName != "" {
			if err != nil {
				return err
			}
			reports = append(reports, keppel.StoredTrivyReportInfo{
				RepositoryName: repoName,
				Digest:         manifestDigest,
				Format:         format,
			})
			return nil
		}
		return fmt.Errorf("encountered unexpected object while listing storage contents of account %s: %s", account.Name, o.Name())
	})
	if err != nil {
		return nil, nil, nil, err
	}

	blobs := make([]keppel.StoredBlobInfo, 0, len(chunkCounts))
	for storageID, chunkCount := range chunkCounts {
		blobs = append(blobs, keppel.StoredBlobInfo{
			StorageID:  storageID,
			ChunkCount: chunkCount,
		})
	}

	return blobs, manifests, reports, nil
}

// See comment on keppel.StoredBlobInfo.ChunkCount for explanation of semantics.
func mergeChunkCount(chunkCounts map[string]uint32, key string, chunkNumber uint32) {
	prevCount, exists := chunkCounts[key]
	if !exists {
		// nothing to merge, just record the new value
		chunkCounts[key] = chunkNumber
		return
	}

	// The value 0 indicates a finalized blob and therefore takes precedence over actual chunk numbers.
	if prevCount == 0 || chunkNumber == 0 {
		chunkCounts[key] = 0
		return
	}
	// If 0 is not involved, remember the largest chunk number as that's gonna be the chunk count.
	if chunkNumber > prevCount {
		chunkCounts[key] = chunkNumber
	}
}

// CanSetupAccount implements the keppel.StorageDriver interface.
func (d *swiftDriver) CanSetupAccount(ctx context.Context, account models.ReducedAccount) error {
	// check that the Swift account is accessible
	_, err := d.getBackendAccount(account).Headers(ctx)
	switch {
	case err == nil:
		return nil
	case schwift.Is(err, http.StatusNotFound):
		// 404 can happen when Swift does not have account autocreation enabled. In
		// this case, the account needs to be created, usually through some
		// administrative process.
		return errors.New("Swift storage is not enabled in this project") //nolint:staticcheck // "Swift" is a product name and must be capitalized
	default:
		return err
	}
}

// CleanupAccount implements the keppel.StorageDriver interface.
func (d *swiftDriver) CleanupAccount(ctx context.Context, account models.ReducedAccount) error {
	c, _, err := d.getBackendConnection(ctx, account)
	if err != nil {
		return err
	}
	return c.Delete(ctx, nil)
}
