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

package keppel

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"

	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/pluggable"

	"github.com/sapcc/keppel/internal/models"
)

// StorageDriver is the abstract interface for a multi-tenant-capable storage
// backend.
type StorageDriver interface {
	pluggable.Plugin
	// Init is called before any other interface methods, and allows the plugin to
	// perform first-time initialization.
	//
	// Implementations should inspect the auth driver to ensure that the
	// federation driver can work with this authentication method, or return
	// ErrAuthDriverMismatch otherwise.
	Init(AuthDriver, Configuration) error

	// `storageID` identifies blobs within an account. (The storage ID is
	// different from the digest: The storage ID gets chosen at the start of the
	// upload, when we don't know the full digest yet.) `chunkNumber` identifies
	// how often AppendToBlob() has already been called for this account and
	// storageID. For the first call to AppendToBlob(), `chunkNumber` will be 1.
	// The second call will have a `chunkNumber` of 2, and so on.
	//
	// If `chunkLength` is non-nil, the implementation may assume that `chunk`
	// will yield that many bytes, and return keppel.ErrSizeInvalid when that
	// turns out not to be true.
	AppendToBlob(account models.Account, storageID string, chunkNumber uint32, chunkLength *uint64, chunk io.Reader) error
	// FinalizeBlob() is called at the end of the upload, after the last
	// AppendToBlob() call for that blob. `chunkCount` identifies how often
	// AppendToBlob() was called.
	FinalizeBlob(account models.Account, storageID string, chunkCount uint32) error
	// AbortBlobUpload() is used to clean up after an error in AppendToBlob() or
	// FinalizeBlob(). It is the counterpart of DeleteBlob() for when any part of
	// the blob upload failed.
	AbortBlobUpload(account models.Account, storageID string, chunkCount uint32) error

	ReadBlob(account models.Account, storageID string) (contents io.ReadCloser, sizeBytes uint64, err error)
	// If the blob can be retrieved by a publicly accessible URL, URLForBlob shall
	// return it. Otherwise ErrCannotGenerateURL shall be returned to instruct the
	// caller fall back to ReadBlob().
	URLForBlob(account models.Account, storageID string) (string, error)
	// DeleteBlob may assume that FinalizeBlob() has been called. If an error
	// occurred before or during FinalizeBlob(), AbortBlobUpload() will be called
	// instead.
	DeleteBlob(account models.Account, storageID string) error

	ReadManifest(account models.Account, repoName string, digest digest.Digest) ([]byte, error)
	WriteManifest(account models.Account, repoName string, digest digest.Digest, contents []byte) error
	DeleteManifest(account models.Account, repoName string, digest digest.Digest) error

	// This method shall only be used as a positive signal for the existence of a
	// blob or manifest in the storage, not as a negative signal: If we expect a
	// blob or manifest to be in the storage, but it does not show up in these
	// lists, that does not necessarily mean it does not exist in the storage.
	// This is because storage implementations may be backed by object stores with
	// eventual consistency.
	ListStorageContents(account models.Account) (blobs []StoredBlobInfo, manifests []StoredManifestInfo, err error)

	// This method is called before a new account is set up in the DB. The
	// StorageDriver can use this opportunity to check for any reasons why the
	// account would not be functional once it is persisted in our DB.
	CanSetupAccount(account models.Account) error
	// This method can be used by the StorageDriver to perform last-minute cleanup
	// on an account that we are about to delete. This cleanup should be
	// reversible; we might bail out of the account deletion afterwards if the
	// deletion in the DB fails.
	CleanupAccount(account models.Account) error
}

// StoredBlobInfo is returned by StorageDriver.ListStorageContents().
type StoredBlobInfo struct {
	StorageID string
	// ChunkCount is 0 for finalized blobs (that can be deleted with DeleteBlob)
	// or >0 for ongoing uploads (that can be deleted with AbortBlobUpload).
	ChunkCount uint32
}

// StoredManifestInfo is returned by StorageDriver.ListStorageContents().
type StoredManifestInfo struct {
	RepoName string
	Digest   digest.Digest
}

// ErrAuthDriverMismatch is returned by Init() methods on most driver
// interfaces, to indicate that the driver in question does not work with the
// selected AuthDriver.
var ErrAuthDriverMismatch = errors.New("given AuthDriver is not supported by this driver")

// ErrCannotGenerateURL is returned by StorageDriver.URLForBlob() when the
// StorageDriver does not support blob URLs.
var ErrCannotGenerateURL = errors.New("URLForBlob() is not supported")

// StorageDriverRegistry is a pluggable.Registry for StorageDriver implementations.
var StorageDriverRegistry pluggable.Registry[StorageDriver]

// NewStorageDriver creates a new StorageDriver using one of the factory functions
// registered with RegisterStorageDriver().
func NewStorageDriver(pluginTypeID string, ad AuthDriver, cfg Configuration) (StorageDriver, error) {
	logg.Debug("initializing storage driver %q...", pluginTypeID)

	sd := StorageDriverRegistry.Instantiate(pluginTypeID)
	if sd == nil {
		return nil, errors.New("no such storage driver: " + pluginTypeID)
	}
	return sd, sd.Init(ad, cfg)
}

// GenerateStorageID generates a new random storage ID for use with
// keppel.StorageDriver.AppendToBlob().
func GenerateStorageID() string {
	buf := make([]byte, 32)
	_, err := rand.Read(buf)
	if err != nil {
		panic(err.Error())
	}
	return hex.EncodeToString(buf)
}
