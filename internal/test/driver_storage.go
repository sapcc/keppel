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

package test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"regexp"

	"github.com/sapcc/keppel/internal/keppel"
)

func init() {
	keppel.RegisterStorageDriver("in-memory-for-testing", func(_ keppel.AuthDriver, _ keppel.Configuration) (keppel.StorageDriver, error) {
		return &StorageDriver{make(map[string][]byte), make(map[string]uint32), make(map[string][]byte), false}, nil
	})
}

//StorageDriver (driver ID "in-memory-for-testing") is a keppel.StorageDriver
//for use in test suites where each keppel-registry stores its contents in RAM
//only, without any persistence.
type StorageDriver struct {
	blobs           map[string][]byte
	blobChunkCounts map[string]uint32 //previous chunkNumber for running upload, 0 when finished (same semantics as keppel.StoredBlobInfo.ChunkCount field)
	manifests       map[string][]byte
	AllowDummyURLs  bool
}

var (
	errNoSuchBlob                   = errors.New("no such blob")
	errNoSuchManifest               = errors.New("no such manifest")
	errAppendToBlobAfterFinalize    = errors.New("AppendToBlob() was called after FinalizeBlob()")
	errAbortBlobUploadAfterFinalize = errors.New("AbortBlobUpload() was called after FinalizeBlob()")
)

func blobKey(account keppel.Account, storageID string) string {
	return fmt.Sprintf("%s/%s", account.Name, storageID)
}

func manifestKey(account keppel.Account, repoName, digest string) string {
	return fmt.Sprintf("%s/%s/%s", account.Name, repoName, digest)
}

//AppendToBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) AppendToBlob(account keppel.Account, storageID string, chunkNumber uint32, chunkLength *uint64, chunk io.Reader) error {
	k := blobKey(account, storageID)

	//check that we're calling AppendToBlob() in the correct order
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

	chunkBytes, err := ioutil.ReadAll(chunk)
	if err != nil {
		return err
	}
	d.blobs[k] = append(d.blobs[k], chunkBytes...)
	d.blobChunkCounts[k] = chunkNumber
	return nil
}

//FinalizeBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) FinalizeBlob(account keppel.Account, storageID string, chunkCount uint32) error {
	k := blobKey(account, storageID)
	_, exists := d.blobs[k]
	if !exists {
		return errNoSuchBlob
	}
	d.blobChunkCounts[k] = 0 //mark as finalized
	return nil
}

//AbortBlobUpload implements the keppel.StorageDriver interface.
func (d *StorageDriver) AbortBlobUpload(account keppel.Account, storageID string, chunkCount uint32) error {
	if d.blobChunkCounts[blobKey(account, storageID)] == 0 {
		return errAbortBlobUploadAfterFinalize
	}
	return d.DeleteBlob(account, storageID)
}

//ReadBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) ReadBlob(account keppel.Account, storageID string) (io.ReadCloser, uint64, error) {
	contents, exists := d.blobs[blobKey(account, storageID)]
	if !exists {
		return nil, 0, errNoSuchBlob
	}
	return ioutil.NopCloser(bytes.NewReader(contents)), uint64(len(contents)), nil
}

//URLForBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) URLForBlob(account keppel.Account, storageID string) (string, error) {
	if d.AllowDummyURLs {
		return "blob://" + storageID, nil
	}
	return "", keppel.ErrCannotGenerateURL
}

//DeleteBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) DeleteBlob(account keppel.Account, storageID string) error {
	k := blobKey(account, storageID)
	_, exists := d.blobs[k]
	if !exists {
		return errNoSuchBlob
	}
	delete(d.blobs, k)
	delete(d.blobChunkCounts, k)
	return nil
}

//ReadManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) ReadManifest(account keppel.Account, repoName, digest string) ([]byte, error) {
	k := manifestKey(account, repoName, digest)
	contents, exists := d.manifests[k]
	if !exists {
		return nil, errNoSuchManifest
	}
	return contents, nil
}

//WriteManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) WriteManifest(account keppel.Account, repoName, digest string, contents []byte) error {
	k := manifestKey(account, repoName, digest)
	d.manifests[k] = contents
	return nil
}

//DeleteManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) DeleteManifest(account keppel.Account, repoName, digest string) error {
	k := manifestKey(account, repoName, digest)
	_, exists := d.manifests[k]
	if !exists {
		return errNoSuchManifest
	}
	delete(d.manifests, k)
	return nil
}

//ListStorageContents implements the keppel.StorageDriver interface.
func (d *StorageDriver) ListStorageContents(account keppel.Account) ([]keppel.StoredBlobInfo, []keppel.StoredManifestInfo, error) {
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
			manifests = append(manifests, keppel.StoredManifestInfo{
				RepoName: match[1],
				Digest:   match[2],
			})
		}
	}

	return blobs, manifests, nil
}

//BlobCount returns how many blobs exist in this storage driver. This is mostly
//used to validate that failure cases do not commit data to the storage.
func (d *StorageDriver) BlobCount() int {
	return len(d.blobs)
}

//ManifestCount returns how many manifests exist in this storage driver. This is mostly
//used to validate that failure cases do not commit data to the storage.
func (d *StorageDriver) ManifestCount() int {
	return len(d.manifests)
}
