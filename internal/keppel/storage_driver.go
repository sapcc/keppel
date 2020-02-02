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
)

//StorageDriver is the abstract interface for a multi-tenant-capable storage
//backend.
type StorageDriver interface {
	//`storageID` identifies blobs within an account. (The storage ID is
	//different from the digest: The storage ID gets chosen at the start of the
	//upload, when we don't know the full digest yet.) `chunkNumber` identifies
	//how often AppendToBlob() has already been called for this account and
	//storageID. For the first call to AppendToBlob(), `chunkNumber` will be 1.
	//The second call will have a `chunkNumber` of 2, and so on.
	//
	//If `chunkLength` is non-nil, the implementation may assume that `chunk`
	//will yield that many bytes, and return keppel.ErrSizeInvalid when that
	//turns out not to be true.
	AppendToBlob(account Account, storageID string, chunkNumber uint32, chunkLength *uint64, chunk io.Reader) error
	//FinalizeBlob() is called at the end of the upload, after the last
	//AppendToBlob() call for that blob. `chunkCount` identifies how often
	//AppendToBlob() was called.
	FinalizeBlob(account Account, storageID string, chunkCount uint32) error
	//AbortBlobUpload() is used to clean up after an error in AppendToBlob() or
	//FinalizeBlob(). It is the counterpart of DeleteBlob() for when any part of
	//the blob upload failed.
	AbortBlobUpload(account Account, storageID string, chunkCount uint32) error

	ReadBlob(account Account, storageID string) (contents io.ReadCloser, sizeBytes uint64, err error)
	//If the blob can be retrieved by a publicly accessible URL, URLForBlob shall
	//return it. Otherwise ErrCannotGenerateURL shall be returned to instruct the
	//caller fall back to ReadBlob().
	URLForBlob(account Account, storageID string) (string, error)
	//DeleteBlob may assume that FinalizeBlob() has been called. If an error
	//occurred before or during FinalizeBlob(), AbortBlobUpload() will be called
	//instead.
	DeleteBlob(account Account, storageID string) error

	ReadManifest(account Account, repoName, digest string) ([]byte, error)
	WriteManifest(account Account, repoName, digest string, contents []byte) error
	DeleteManifest(account Account, repoName, digest string) error
}

//ErrAuthDriverMismatch can be returned by StorageDriver and NameClaimDriver.
var ErrAuthDriverMismatch = errors.New("given AuthDriver is not supported by this driver")

//ErrCannotGenerateURL is returned by StorageDriver.URLForBlob() when the
//StorageDriver does not support blob URLs.
var ErrCannotGenerateURL = errors.New("URLForBlob() is not supported")

var storageDriverFactories = make(map[string]func(AuthDriver, Configuration) (StorageDriver, error))

//NewStorageDriver creates a new StorageDriver using one of the factory functions
//registered with RegisterStorageDriver().
func NewStorageDriver(name string, authDriver AuthDriver, cfg Configuration) (StorageDriver, error) {
	factory := storageDriverFactories[name]
	if factory != nil {
		return factory(authDriver, cfg)
	}
	return nil, errors.New("no such storage driver: " + name)
}

//RegisterStorageDriver registers an StorageDriver. Call this from func init() of the
//package defining the StorageDriver.
//
//Factory implementations should inspect the auth driver to ensure that the
//storage backend can work with this authentication method, returning
//ErrAuthDriverMismatch otherwise.
func RegisterStorageDriver(name string, factory func(AuthDriver, Configuration) (StorageDriver, error)) {
	if _, exists := storageDriverFactories[name]; exists {
		panic("attempted to register multiple storage drivers with name = " + name)
	}
	storageDriverFactories[name] = factory
}

//GenerateStorageID generates a new random storage ID for use with keppel.StorageDriver.AppendToBlob().
func GenerateStorageID() string {
	buf := make([]byte, 32)
	_, err := rand.Read(buf)
	if err != nil {
		panic(err.Error())
	}
	return hex.EncodeToString(buf)
}
