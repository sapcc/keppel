/******************************************************************************
*
*  Copyright 2020 SAP SE
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
	"io"
	"testing"

	"github.com/sapcc/keppel/internal/keppel"
)

// ExpectBlobsExistInStorage is a test assertion.
func (s Setup) ExpectBlobsExistInStorage(t *testing.T, blobs ...keppel.Blob) {
	t.Helper()
	for _, blob := range blobs {
		account := keppel.Account{Name: blob.AccountName}
		readCloser, sizeBytes, err := s.SD.ReadBlob(account, blob.StorageID)
		if err != nil {
			t.Errorf("expected blob %s to exist in the storage, but got: %s", blob.Digest, err.Error())
			continue
		}
		blobBytes, err := io.ReadAll(readCloser)
		if err == nil {
			readCloser.Close()
		} else {
			err = readCloser.Close()
		}
		if err != nil {
			t.Errorf("unexpected error while reading blob %s: %s", blob.Digest, err.Error())
			continue
		}

		if uint64(len(blobBytes)) != sizeBytes {
			t.Errorf("unexpected error while reading blob %s: expected %d bytes, but got %d bytes", blob.Digest, sizeBytes, len(blobBytes))
			continue
		}

		err = blob.Digest.Validate()
		if err != nil {
			t.Errorf("blob digest %q is not a digest: %s", blob.Digest, err.Error())
			continue
		}

		actualDigest := blob.Digest.Algorithm().FromBytes(blobBytes)
		if actualDigest != blob.Digest {
			t.Errorf("blob %s has corrupted contents: actual digest is %s", blob.Digest, actualDigest)
			continue
		}
	}
}

// ExpectBlobsMissingInStorage is a test assertion.
func (s Setup) ExpectBlobsMissingInStorage(t *testing.T, blobs ...keppel.Blob) {
	t.Helper()
	for _, blob := range blobs {
		account := keppel.Account{Name: blob.AccountName}
		_, _, err := s.SD.ReadBlob(account, blob.StorageID)
		if err == nil {
			t.Errorf("expected blob %s to be missing in the storage, but could read it", blob.Digest)
			continue
		}
	}
}

// ExpectManifestsExistInStorage is a test assertion.
func (s Setup) ExpectManifestsExistInStorage(t *testing.T, repoName string, manifests ...keppel.Manifest) {
	t.Helper()
	for _, manifest := range manifests {
		repo, err := keppel.FindRepositoryByID(s.DB, manifest.RepositoryID)
		mustDo(t, err)
		account := keppel.Account{Name: repo.AccountName}
		manifestBytes, err := s.SD.ReadManifest(account, repoName, manifest.Digest)
		if err != nil {
			t.Errorf("expected manifest %s to exist in the storage, but got: %s", manifest.Digest, err.Error())
			continue
		}
		err = manifest.Digest.Validate()
		if err != nil {
			t.Errorf("manifest digest %q is not a digest: %s", manifest.Digest, err.Error())
			continue
		}
		actualDigest := manifest.Digest.Algorithm().FromBytes(manifestBytes)
		if actualDigest != manifest.Digest {
			t.Errorf("manifest %s has corrupted contents: actual digest is %s", manifest.Digest, actualDigest)
			continue
		}
	}
}

// ExpectManifestsMissingInStorage is a test assertion.
func (s Setup) ExpectManifestsMissingInStorage(t *testing.T, manifests ...keppel.Manifest) {
	t.Helper()
	for _, manifest := range manifests {
		repo, err := keppel.FindRepositoryByID(s.DB, manifest.RepositoryID)
		mustDo(t, err)
		account := keppel.Account{Name: repo.AccountName}
		_, err = s.SD.ReadManifest(account, "foo", manifest.Digest)
		if err == nil {
			t.Errorf("expected manifest %s to be missing in the storage, but could read it", manifest.Digest)
			continue
		}
	}
}
