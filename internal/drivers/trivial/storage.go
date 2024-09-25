/******************************************************************************
*
*  Copyright 2019 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package trivial

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/opencontainers/go-digest"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

func init() {
	keppel.StorageDriverRegistry.Add(func() keppel.StorageDriver { return &StorageDriver{} })
}

// StorageDriver (driver ID "in-memory-for-testing") is a keppel.StorageDriver
// for use in test suites where each keppel-registry stores its contents in RAM
// only, without any persistence.
type StorageDriver struct {
	blobs             map[string][]byte
	blobChunkCounts   map[string]uint32 // previous chunkNumber for running upload, 0 when finished (same semantics as keppel.StoredBlobInfo.ChunkCount field)
	manifests         map[string][]byte
	ForbidNewAccounts bool
}

// PluginTypeID implements the keppel.StorageDriver interface.
func (d *StorageDriver) PluginTypeID() string { return "in-memory-for-testing" }

// Init implements the keppel.StorageDriver interface.
func (d *StorageDriver) Init(ad keppel.AuthDriver, cfg keppel.Configuration) error {
	d.blobs = make(map[string][]byte)
	d.blobChunkCounts = make(map[string]uint32)
	d.manifests = make(map[string][]byte)
	return nil
}

var (
	errNoSuchBlob                   = errors.New("no such blob")
	errNoSuchManifest               = errors.New("no such manifest")
	errAppendToBlobAfterFinalize    = errors.New("AppendToBlob() was called after FinalizeBlob()")
	errAbortBlobUploadAfterFinalize = errors.New("AbortBlobUpload() was called after FinalizeBlob()")
)

func blobKey(account models.ReducedAccount, storageID string) string {
	return fmt.Sprintf("%s/%s", account.Name, storageID)
}

func manifestKey(account models.ReducedAccount, repoName string, manifestDigest digest.Digest) string {
	return fmt.Sprintf("%s/%s/%s", account.Name, repoName, manifestDigest)
}

// AppendToBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) AppendToBlob(ctx context.Context, account models.ReducedAccount, storageID string, chunkNumber uint32, chunkLength *uint64, chunk io.Reader) error {
	k := blobKey(account, storageID)

	// check that we're calling AppendToBlob() in the correct order
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
	d.blobs[k] = append(d.blobs[k], chunkBytes...)
	d.blobChunkCounts[k] = chunkNumber
	return nil
}

// FinalizeBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) FinalizeBlob(ctx context.Context, account models.ReducedAccount, storageID string, chunkCount uint32) error {
	k := blobKey(account, storageID)
	_, exists := d.blobs[k]
	if !exists {
		return errNoSuchBlob
	}
	d.blobChunkCounts[k] = 0 // mark as finalized
	return nil
}

// AbortBlobUpload implements the keppel.StorageDriver interface.
func (d *StorageDriver) AbortBlobUpload(ctx context.Context, account models.ReducedAccount, storageID string, chunkCount uint32) error {
	if d.blobChunkCounts[blobKey(account, storageID)] == 0 {
		return errAbortBlobUploadAfterFinalize
	}
	return d.DeleteBlob(ctx, account, storageID)
}

// ReadBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) ReadBlob(ctx context.Context, account models.ReducedAccount, storageID string) (io.ReadCloser, uint64, error) {
	contents, exists := d.blobs[blobKey(account, storageID)]
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
	k := blobKey(account, storageID)
	_, exists := d.blobs[k]
	if !exists {
		return errNoSuchBlob
	}
	delete(d.blobs, k)
	delete(d.blobChunkCounts, k)
	return nil
}

// ReadManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) ReadManifest(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest) ([]byte, error) {
	k := manifestKey(account, repoName, manifestDigest)
	contents, exists := d.manifests[k]
	if !exists {
		return nil, errNoSuchManifest
	}
	return contents, nil
}

// WriteManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) WriteManifest(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, contents []byte) error {
	k := manifestKey(account, repoName, manifestDigest)
	d.manifests[k] = contents
	return nil
}

// DeleteManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) DeleteManifest(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest) error {
	k := manifestKey(account, repoName, manifestDigest)
	_, exists := d.manifests[k]
	if !exists {
		return errNoSuchManifest
	}
	delete(d.manifests, k)
	return nil
}

// ListStorageContents implements the keppel.StorageDriver interface.
func (d *StorageDriver) ListStorageContents(ctx context.Context, account models.ReducedAccount) ([]keppel.StoredBlobInfo, []keppel.StoredManifestInfo, error) {
	var (
		blobs     []keppel.StoredBlobInfo
		manifests []keppel.StoredManifestInfo
	)

	rx := regexp.MustCompile(`^` + blobKey(account, `(.*)`) + `$`)
	for key := range d.blobs {
		match := rx.FindStringSubmatch(key)
		if match != nil {
			blobs = append(blobs, keppel.StoredBlobInfo{
				StorageID:  match[1],
				ChunkCount: d.blobChunkCounts[key],
			})
		}
	}

	rx = regexp.MustCompile(`^` + manifestKey(account, `(.*)`, `(.*)`) + `$`)
	for key := range d.manifests {
		match := rx.FindStringSubmatch(key)
		if match != nil {
			manifestDigest, err := digest.Parse(match[2])
			if err != nil {
				return nil, nil, err
			}
			manifests = append(manifests, keppel.StoredManifestInfo{
				RepoName: match[1],
				Digest:   manifestDigest,
			})
		}
	}

	return blobs, manifests, nil
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
	storedBlobs, storedManifests, err := d.ListStorageContents(ctx, account)
	if len(storedBlobs) > 0 {
		return fmt.Errorf(
			"found undeleted blob during CleanupAccount: storageID = %q",
			storedBlobs[0].StorageID,
		)
	}
	if len(storedManifests) > 0 {
		return fmt.Errorf(
			"found undeleted manifest during CleanupAccount: %s@%s",
			storedManifests[0].RepoName,
			storedManifests[0].Digest,
		)
	}
	return err
}

// BlobCount returns how many blobs exist in this storage driver. This is mostly
// used to validate that failure cases do not commit data to the storage.
func (d *StorageDriver) BlobCount() int {
	return len(d.blobs)
}

// ManifestCount returns how many manifests exist in this storage driver. This is mostly
// used to validate that failure cases do not commit data to the storage.
func (d *StorageDriver) ManifestCount() int {
	return len(d.manifests)
}
