// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package trivial

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	. "github.com/majewsky/gg/option"
	"github.com/opencontainers/go-digest"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/trivy"
)

func init() {
	keppel.StorageDriverRegistry.Add(func() keppel.StorageDriver { return &StorageDriver{} })
}

// StorageDriver (driver ID "in-memory-for-testing") is a keppel.StorageDriver
// for use in test suites where each keppel-registry stores its contents in RAM
// only, without any persistence.
type StorageDriver struct {
	// configuration within tests
	ForbidNewAccounts bool `json:"-"`

	// state
	blobs                map[blobKey][]byte
	blobsMutex           sync.RWMutex
	blobChunkCounts      map[blobKey]uint32 // previous chunkNumber for running upload, 0 when finished (same semantics as keppel.StoredBlobInfo.ChunkCount field)
	blobChunkCountsMutex sync.RWMutex
	manifests            map[manifestKey][]byte
	manifestMutex        sync.RWMutex
	trivyReports         map[trivyReportKey][]byte
	trivyReportsMutex    sync.RWMutex
}

type blobKey struct {
	AuthTenantID string
	AccountName  models.AccountName
	StorageID    string
}

type manifestKey struct {
	AuthTenantID   string
	AccountName    models.AccountName
	RepositoryName string
	Digest         digest.Digest
}

type trivyReportKey struct {
	manifestKey
	Format string
}

// PluginTypeID implements the keppel.StorageDriver interface.
func (d *StorageDriver) PluginTypeID() string { return "in-memory-for-testing" }

// Init implements the keppel.StorageDriver interface.
func (d *StorageDriver) Init(ad keppel.AuthDriver, cfg keppel.Configuration) error {
	d.blobs = make(map[blobKey][]byte)
	d.blobChunkCounts = make(map[blobKey]uint32)
	d.manifests = make(map[manifestKey][]byte)
	d.trivyReports = make(map[trivyReportKey][]byte)
	return nil
}

var (
	errNoSuchBlob                   = errors.New("no such blob")
	errNoSuchManifest               = errors.New("no such manifest")
	errNoSuchTrivyReport            = errors.New("no such Trivy report")
	errAppendToBlobAfterFinalize    = errors.New("AppendToBlob() was called after FinalizeBlob()")
	errAbortBlobUploadAfterFinalize = errors.New("AbortBlobUpload() was called after FinalizeBlob()")
)

func checkAccount(account models.ReducedAccount) error {
	if account.Name == "" || account.AuthTenantID == "" {
		return fmt.Errorf("invalid account: name = %q, authTenantID = %q", account.Name, account.AuthTenantID)
	}
	return nil
}

func getBlobKey(account models.ReducedAccount, storageID string) (blobKey, error) {
	err := checkAccount(account)
	if err != nil {
		return blobKey{}, err
	}
	return blobKey{
		AuthTenantID: account.AuthTenantID,
		AccountName:  account.Name,
		StorageID:    storageID,
	}, nil
}

func getManifestKey(account models.ReducedAccount, repoName string, manifestDigest digest.Digest) (manifestKey, error) {
	err := checkAccount(account)
	if err != nil {
		return manifestKey{}, err
	}
	return manifestKey{
		AuthTenantID:   account.AuthTenantID,
		AccountName:    account.Name,
		RepositoryName: repoName,
		Digest:         manifestDigest,
	}, nil
}

func getTrivyReportKey(account models.ReducedAccount, repoName string, manifestDigest digest.Digest, format string) (trivyReportKey, error) {
	mk, err := getManifestKey(account, repoName, manifestDigest)
	if err != nil {
		return trivyReportKey{}, err
	}
	return trivyReportKey{mk, format}, nil
}

// AppendToBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) AppendToBlob(ctx context.Context, account models.ReducedAccount, storageID string, chunkNumber uint32, chunkLength Option[uint64], chunk io.Reader) error {
	k, err := getBlobKey(account, storageID)
	if err != nil {
		return err
	}

	// check that we're calling AppendToBlob() in the correct order
	d.blobChunkCountsMutex.Lock()
	defer d.blobChunkCountsMutex.Unlock()
	chunkCount, exists := d.blobChunkCounts[k]
	if chunkNumber == 1 {
		if exists {
			return fmt.Errorf("expected chunk #%d, but got chunk #1", chunkCount+1)
		}
	} else {
		if exists && chunkCount == 0 {
			return errAppendToBlobAfterFinalize
		}
		if chunkCount+1 != chunkNumber || !exists {
			return fmt.Errorf("expected chunk #%d, but got chunk #%d", chunkCount+1, chunkNumber)
		}
	}

	chunkBytes, err := io.ReadAll(chunk)
	if err != nil {
		return err
	}

	d.blobsMutex.Lock()
	defer d.blobsMutex.Unlock()
	d.blobs[k] = append(d.blobs[k], chunkBytes...)
	d.blobChunkCounts[k] = chunkNumber
	return nil
}

// FinalizeBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) FinalizeBlob(ctx context.Context, account models.ReducedAccount, storageID string, chunkCount uint32) error {
	k, err := getBlobKey(account, storageID)
	if err != nil {
		return err
	}

	d.blobsMutex.RLock()
	defer d.blobsMutex.RUnlock()
	_, exists := d.blobs[k]
	if !exists {
		return errNoSuchBlob
	}
	d.blobChunkCountsMutex.Lock()
	defer d.blobChunkCountsMutex.Unlock()
	d.blobChunkCounts[k] = 0 // mark as finalized
	return nil
}

// AbortBlobUpload implements the keppel.StorageDriver interface.
func (d *StorageDriver) AbortBlobUpload(ctx context.Context, account models.ReducedAccount, storageID string, chunkCount uint32) error {
	k, err := getBlobKey(account, storageID)
	if err != nil {
		return err
	}
	d.blobChunkCountsMutex.RLock()
	// we need to unlock here early because DeleteBlob() will also try to lock blobChunkCountsMutex
	if d.blobChunkCounts[k] == 0 {
		d.blobChunkCountsMutex.RUnlock()
		return errAbortBlobUploadAfterFinalize
	}
	d.blobChunkCountsMutex.RUnlock()
	return d.DeleteBlob(ctx, account, storageID)
}

// ReadBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) ReadBlob(ctx context.Context, account models.ReducedAccount, storageID string) (io.ReadCloser, uint64, error) {
	d.blobsMutex.RLock()
	defer d.blobsMutex.RUnlock()
	blobKey, err := getBlobKey(account, storageID)
	if err != nil {
		return nil, 0, err
	}
	contents, exists := d.blobs[blobKey]
	if !exists {
		return nil, 0, errNoSuchBlob
	}
	return io.NopCloser(bytes.NewReader(contents)), uint64(len(contents)), nil
}

// URLForBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) URLForBlob(ctx context.Context, account models.ReducedAccount, storageID string) (string, error) {
	return "", keppel.ErrCannotGenerateURL
}

// DeleteBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) DeleteBlob(ctx context.Context, account models.ReducedAccount, storageID string) error {
	k, err := getBlobKey(account, storageID)
	if err != nil {
		return err
	}
	d.blobsMutex.Lock()
	defer d.blobsMutex.Unlock()
	_, exists := d.blobs[k]
	if !exists {
		return errNoSuchBlob
	}
	delete(d.blobs, k)
	d.blobChunkCountsMutex.Lock()
	defer d.blobChunkCountsMutex.Unlock()
	delete(d.blobChunkCounts, k)
	return nil
}

// ReadManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) ReadManifest(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest) ([]byte, error) {
	k, err := getManifestKey(account, repoName, manifestDigest)
	if err != nil {
		return nil, err
	}
	d.manifestMutex.RLock()
	defer d.manifestMutex.RUnlock()
	contents, exists := d.manifests[k]
	if !exists {
		return nil, errNoSuchManifest
	}
	return contents, nil
}

// WriteManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) WriteManifest(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, contents []byte) error {
	k, err := getManifestKey(account, repoName, manifestDigest)
	if err != nil {
		return err
	}
	d.manifestMutex.Lock()
	defer d.manifestMutex.Unlock()
	d.manifests[k] = contents
	return nil
}

// DeleteManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) DeleteManifest(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest) error {
	k, err := getManifestKey(account, repoName, manifestDigest)
	if err != nil {
		return err
	}
	d.manifestMutex.Lock()
	defer d.manifestMutex.Unlock()
	_, exists := d.manifests[k]
	if !exists {
		return errNoSuchManifest
	}
	delete(d.manifests, k)
	return nil
}

// ReadTrivyReport implements the keppel.StorageDriver interface.
func (d *StorageDriver) ReadTrivyReport(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, format string) (io.ReadCloser, error) {
	k, err := getTrivyReportKey(account, repoName, manifestDigest, format)
	if err != nil {
		return nil, err
	}
	d.trivyReportsMutex.RLock()
	defer d.trivyReportsMutex.RUnlock()
	contents, exists := d.trivyReports[k]
	if !exists {
		return nil, errNoSuchTrivyReport
	}
	return io.NopCloser(bytes.NewReader(contents)), nil
}

// WriteTrivyReport implements the keppel.StorageDriver interface.
func (d *StorageDriver) WriteTrivyReport(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, payload trivy.ReportPayload) error {
	k, err := getTrivyReportKey(account, repoName, manifestDigest, payload.Format)
	if err != nil {
		return err
	}
	d.trivyReportsMutex.Lock()
	defer d.trivyReportsMutex.Unlock()
	report, err := io.ReadAll(payload.Contents)
	if err != nil {
		return err
	}
	if len(report) == 0 {
		return errors.New("found empty report")
	}
	d.trivyReports[k] = report
	return nil
}

// DeleteTrivyReport implements the keppel.StorageDriver interface.
func (d *StorageDriver) DeleteTrivyReport(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, format string) error {
	k, err := getTrivyReportKey(account, repoName, manifestDigest, format)
	if err != nil {
		return err
	}
	d.trivyReportsMutex.Lock()
	defer d.trivyReportsMutex.Unlock()
	_, exists := d.trivyReports[k]
	if !exists {
		return errNoSuchTrivyReport
	}
	delete(d.trivyReports, k)
	return nil
}

// ListStorageContents implements the keppel.StorageDriver interface.
func (d *StorageDriver) ListStorageContents(ctx context.Context, account models.ReducedAccount) ([]keppel.StoredBlobInfo, []keppel.StoredManifestInfo, []keppel.StoredTrivyReportInfo, error) {
	err := checkAccount(account)
	if err != nil {
		return nil, nil, nil, err
	}

	var (
		blobs        []keppel.StoredBlobInfo
		manifests    []keppel.StoredManifestInfo
		trivyReports []keppel.StoredTrivyReportInfo
	)

	d.blobChunkCountsMutex.RLock()
	defer d.blobChunkCountsMutex.RUnlock()
	d.blobsMutex.RLock()
	defer d.blobsMutex.RUnlock()
	for key := range d.blobs {
		if key.AccountName == account.Name && key.AuthTenantID == account.AuthTenantID {
			blobs = append(blobs, keppel.StoredBlobInfo{
				StorageID:  key.StorageID,
				ChunkCount: d.blobChunkCounts[key],
			})
		}
	}

	d.manifestMutex.RLock()
	defer d.manifestMutex.RUnlock()
	for key := range d.manifests {
		if key.AccountName == account.Name && key.AuthTenantID == account.AuthTenantID {
			manifests = append(manifests, keppel.StoredManifestInfo{
				RepositoryName: key.RepositoryName,
				Digest:         key.Digest,
			})
		}
	}

	d.trivyReportsMutex.RLock()
	defer d.trivyReportsMutex.RUnlock()
	for key := range d.trivyReports {
		if key.AccountName == account.Name && key.AuthTenantID == account.AuthTenantID {
			trivyReports = append(trivyReports, keppel.StoredTrivyReportInfo{
				RepositoryName: key.RepositoryName,
				Digest:         key.Digest,
				Format:         key.Format,
			})
		}
	}

	return blobs, manifests, trivyReports, nil
}

// UsedBytes implements the keppel.StorageDriver interface.
func (d *StorageDriver) UsedBytes(ctx context.Context, authTenantID string) (usedBytes uint64, err error) {
	d.blobsMutex.RLock()
	defer d.blobsMutex.RUnlock()
	for key := range d.blobs {
		if key.AuthTenantID == authTenantID {
			usedBytes += uint64(len(d.blobs[key]))
		}
	}

	d.manifestMutex.RLock()
	defer d.manifestMutex.RUnlock()
	for key := range d.manifests {
		if key.AuthTenantID == authTenantID {
			usedBytes += uint64(len(d.manifests[key]))
		}
	}

	d.trivyReportsMutex.RLock()
	defer d.trivyReportsMutex.RUnlock()
	for key := range d.trivyReports {
		if key.AuthTenantID == authTenantID {
			usedBytes += uint64(len(d.trivyReports[key]))
		}
	}

	return usedBytes, nil
}

// CanSetupAccount implements the keppel.StorageDriver interface.
func (d *StorageDriver) CanSetupAccount(ctx context.Context, account models.ReducedAccount) error {
	if d.ForbidNewAccounts {
		return errors.New("CanSetupAccount failed as requested")
	}
	return nil
}

// CleanupAccount implements the keppel.StorageDriver interface.
func (d *StorageDriver) CleanupAccount(ctx context.Context, account models.ReducedAccount) error {
	// double-check that cleanup order is right; when the account gets deleted,
	// all blobs and manifests must have been deleted from it before
	storedBlobs, storedManifests, storedTrivyReports, err := d.ListStorageContents(ctx, account)
	if len(storedBlobs) > 0 {
		return fmt.Errorf(
			"found undeleted blob during CleanupAccount: storageID = %q",
			storedBlobs[0].StorageID,
		)
	}
	if len(storedManifests) > 0 {
		return fmt.Errorf(
			"found undeleted manifest during CleanupAccount: %s@%s",
			storedManifests[0].RepositoryName,
			storedManifests[0].Digest,
		)
	}
	if len(storedTrivyReports) > 0 {
		return fmt.Errorf(
			"found undeleted Trivy report during CleanupAccount: %s@%s --format %s",
			storedTrivyReports[0].RepositoryName,
			storedTrivyReports[0].Digest,
			storedTrivyReports[0].Format,
		)
	}
	return err
}

// BlobCount returns how many blobs exist in this storage driver. This is mostly
// used to validate that failure cases do not commit data to the storage.
func (d *StorageDriver) BlobCount() int {
	d.blobsMutex.RLock()
	defer d.blobsMutex.RUnlock()
	return len(d.blobs)
}

// ManifestCount returns how many manifests exist in this storage driver. This is mostly
// used to validate that failure cases do not commit data to the storage.
func (d *StorageDriver) ManifestCount() int {
	d.manifestMutex.RLock()
	defer d.manifestMutex.RUnlock()
	return len(d.manifests)
}
