// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"io"
	"testing"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// ExpectBlobsExistInStorage is a test assertion.
func (s Setup) ExpectBlobsExistInStorage(t *testing.T, blobs ...models.Blob) {
	t.Helper()
	for _, blob := range blobs {
		account := models.ReducedAccount{Name: blob.AccountName}
		readCloser, sizeBytes, err := s.SD.ReadBlob(s.Ctx, account, blob.StorageID)
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
func (s Setup) ExpectBlobsMissingInStorage(t *testing.T, blobs ...models.Blob) {
	t.Helper()
	for _, blob := range blobs {
		account := models.ReducedAccount{Name: blob.AccountName}
		_, _, err := s.SD.ReadBlob(s.Ctx, account, blob.StorageID)
		if err == nil {
			t.Errorf("expected blob %s to be missing in the storage, but could read it", blob.Digest)
			continue
		}
	}
}

// ExpectManifestsExistInStorage is a test assertion.
func (s Setup) ExpectManifestsExistInStorage(t *testing.T, repoName string, manifests ...models.Manifest) {
	t.Helper()
	for _, manifest := range manifests {
		repo, err := keppel.FindRepositoryByID(s.DB, manifest.RepositoryID)
		MustDo(t, err)
		account := models.ReducedAccount{Name: repo.AccountName}
		manifestBytes, err := s.SD.ReadManifest(s.Ctx, account, repoName, manifest.Digest)
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
func (s Setup) ExpectManifestsMissingInStorage(t *testing.T, manifests ...models.Manifest) {
	t.Helper()
	for _, manifest := range manifests {
		repo, err := keppel.FindRepositoryByID(s.DB, manifest.RepositoryID)
		MustDo(t, err)
		account := models.ReducedAccount{Name: repo.AccountName}
		_, err = s.SD.ReadManifest(s.Ctx, account, "foo", manifest.Digest)
		if err == nil {
			t.Errorf("expected manifest %s to be missing in the storage, but could read it", manifest.Digest)
			continue
		}
	}
}
