/******************************************************************************
*
*  Copyright 2022 ruilopes.com
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

package filesystem

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sapcc/go-bits/osext"

	"github.com/sapcc/keppel/internal/keppel"
)

func init() {
	keppel.RegisterStorageDriver("filesystem", func(_ keppel.AuthDriver, _ keppel.Configuration) (keppel.StorageDriver, error) {
		rootPath, err := filepath.Abs(osext.MustGetenv("KEPPEL_FILESYSTEM_PATH"))
		if err != nil {
			return nil, err
		}
		return &StorageDriver{
			rootPath: rootPath,
		}, nil
	})
}

// StorageDriver (driver ID "filesystem") is a keppel.StorageDriver that stores its contents in the local filesystem.
type StorageDriver struct {
	rootPath string
}

func (d *StorageDriver) getBlobBasePath(account keppel.Account) string {
	return fmt.Sprintf("%s/%s/%s/blobs", d.rootPath, account.AuthTenantID, account.Name)
}

func (d *StorageDriver) getBlobPath(account keppel.Account, storageID string) string {
	return fmt.Sprintf("%s/%s/%s/blobs/%s", d.rootPath, account.AuthTenantID, account.Name, storageID)
}

func (d *StorageDriver) getManifestBasePath(account keppel.Account) string {
	return fmt.Sprintf("%s/%s/%s/manifests", d.rootPath, account.AuthTenantID, account.Name)
}

func (d *StorageDriver) getManifestPath(account keppel.Account, repoName, digest string) string {
	return fmt.Sprintf("%s/%s/%s/manifests/%s/%s", d.rootPath, account.AuthTenantID, account.Name, repoName, digest)
}

// AppendToBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) AppendToBlob(account keppel.Account, storageID string, chunkNumber uint32, chunkLength *uint64, chunk io.Reader) error {
	path := d.getBlobPath(account, storageID)
	tmpPath := path + ".tmp"
	flags := os.O_APPEND | os.O_WRONLY
	if chunkNumber == 1 {
		err := os.MkdirAll(filepath.Dir(tmpPath), 0777)
		if err != nil {
			return err
		}
		flags = flags | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(tmpPath, flags, 0666)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, chunk)
	return err
}

// FinalizeBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) FinalizeBlob(account keppel.Account, storageID string, chunkCount uint32) error {
	path := d.getBlobPath(account, storageID)
	tmpPath := path + ".tmp"
	return os.Rename(tmpPath, path)
}

// AbortBlobUpload implements the keppel.StorageDriver interface.
func (d *StorageDriver) AbortBlobUpload(account keppel.Account, storageID string, chunkCount uint32) error {
	path := d.getBlobPath(account, storageID)
	tmpPath := path + ".tmp"
	return os.Remove(tmpPath)
}

// ReadBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) ReadBlob(account keppel.Account, storageID string) (io.ReadCloser, uint64, error) {
	path := d.getBlobPath(account, storageID)
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, uint64(stat.Size()), nil
}

// URLForBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) URLForBlob(account keppel.Account, storageID string) (string, error) {
	return "", keppel.ErrCannotGenerateURL
}

// DeleteBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) DeleteBlob(account keppel.Account, storageID string) error {
	path := d.getBlobPath(account, storageID)
	return os.Remove(path)
}

// ReadManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) ReadManifest(account keppel.Account, repoName, digest string) ([]byte, error) {
	path := d.getManifestPath(account, repoName, digest)
	return os.ReadFile(path)
}

// WriteManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) WriteManifest(account keppel.Account, repoName, digest string, contents []byte) error {
	path := d.getManifestPath(account, repoName, digest)
	tmpPath := path + ".tmp"
	err := os.MkdirAll(filepath.Dir(tmpPath), 0777)
	if err != nil {
		return err
	}
	err = os.WriteFile(tmpPath, contents, 0666)
	if err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// DeleteManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) DeleteManifest(account keppel.Account, repoName, digest string) error {
	path := d.getManifestPath(account, repoName, digest)
	return os.Remove(path)
}

// ListStorageContents implements the keppel.StorageDriver interface.
func (d *StorageDriver) ListStorageContents(account keppel.Account) ([]keppel.StoredBlobInfo, []keppel.StoredManifestInfo, error) {
	blobs, err := d.getBlobs(account)
	if err != nil {
		return nil, nil, err
	}
	manifests, err := d.getManifests(account)
	if err != nil {
		return nil, nil, err
	}
	return blobs, manifests, nil
}

func (d *StorageDriver) getBlobs(account keppel.Account) ([]keppel.StoredBlobInfo, error) {
	var blobs []keppel.StoredBlobInfo
	directory, err := os.Open(d.getBlobBasePath(account))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []keppel.StoredBlobInfo{}, nil
		}
		return nil, err
	}
	defer directory.Close()
	names, err := directory.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if strings.HasSuffix(name, ".tmp") {
			continue
		}
		blobs = append(blobs, keppel.StoredBlobInfo{
			StorageID: name,
		})
	}
	return blobs, nil
}

func (d *StorageDriver) getManifests(account keppel.Account) ([]keppel.StoredManifestInfo, error) {
	var manifests []keppel.StoredManifestInfo
	directory, err := os.Open(d.getManifestBasePath(account))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []keppel.StoredManifestInfo{}, nil
		}
		return nil, err
	}
	defer directory.Close()
	repos, err := directory.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	for _, repo := range repos {
		repoManifests, err := d.getRepoManifests(account, repo)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, repoManifests...)
	}
	return manifests, nil
}

func (d *StorageDriver) getRepoManifests(account keppel.Account, repo string) ([]keppel.StoredManifestInfo, error) {
	var manifests []keppel.StoredManifestInfo
	directory, err := os.Open(filepath.Join(d.getManifestBasePath(account), repo))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []keppel.StoredManifestInfo{}, nil
		}
		return nil, err
	}
	defer directory.Close()
	digests, err := directory.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	for _, digest := range digests {
		if strings.HasSuffix(digest, ".tmp") {
			continue
		}
		manifests = append(manifests, keppel.StoredManifestInfo{
			RepoName: repo,
			Digest:   digest,
		})
	}
	return manifests, nil
}

// CleanupAccount implements the keppel.StorageDriver interface.
func (d *StorageDriver) CleanupAccount(account keppel.Account) error {
	//double-check that cleanup order is right; when the account gets deleted,
	//all blobs and manifests must have been deleted from it before
	storedBlobs, storedManifests, err := d.ListStorageContents(account)
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
