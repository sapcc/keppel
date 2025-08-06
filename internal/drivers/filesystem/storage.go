// SPDX-FileCopyrightText: 2022 ruilopes.com
// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	. "github.com/majewsky/gg/option"
	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/osext"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/trivy"
)

func init() {
	keppel.StorageDriverRegistry.Add(func() keppel.StorageDriver { return &StorageDriver{} })
}

// StorageDriver (driver ID "filesystem") is a keppel.StorageDriver that stores its contents in the local filesystem.
type StorageDriver struct {
	rootPath string
}

// PluginTypeID implements the keppel.StorageDriver interface.
func (d *StorageDriver) PluginTypeID() string { return "filesystem" }

// Init implements the keppel.StorageDriver interface.
func (d *StorageDriver) Init(ad keppel.AuthDriver, cfg keppel.Configuration) (err error) {
	d.rootPath, err = filepath.Abs(osext.MustGetenv("KEPPEL_FILESYSTEM_PATH"))
	return err
}

func (d *StorageDriver) getBlobBasePath(account models.ReducedAccount) string {
	return fmt.Sprintf("%s/%s/%s/blobs", d.rootPath, account.AuthTenantID, account.Name)
}

func (d *StorageDriver) getBlobPath(account models.ReducedAccount, storageID string) string {
	return fmt.Sprintf("%s/%s", d.getBlobBasePath(account), storageID)
}

func (d *StorageDriver) getManifestBasePath(account models.ReducedAccount) string {
	return fmt.Sprintf("%s/%s/%s/manifests", d.rootPath, account.AuthTenantID, account.Name)
}

func (d *StorageDriver) getManifestPath(account models.ReducedAccount, repoName string, manifestDigest digest.Digest) string {
	return fmt.Sprintf("%s/%s/%s", d.getManifestBasePath(account), repoName, manifestDigest)
}

func (d *StorageDriver) getTrivyReportBasePath(account models.ReducedAccount) string {
	return fmt.Sprintf("%s/%s/%s/trivy-reports", d.rootPath, account.AuthTenantID, account.Name)
}

func (d *StorageDriver) getTrivyReportPath(account models.ReducedAccount, repoName string, manifestDigest digest.Digest, format string) string {
	return fmt.Sprintf("%s/%s/%s/%s", d.getTrivyReportBasePath(account), repoName, manifestDigest, format)
}

// AppendToBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) AppendToBlob(ctx context.Context, account models.ReducedAccount, storageID string, chunkNumber uint32, chunkLength Option[uint64], chunk io.Reader) error {
	path := d.getBlobPath(account, storageID)
	tmpPath := path + ".tmp"
	flags := os.O_APPEND | os.O_WRONLY
	if chunkNumber == 1 {
		err := os.MkdirAll(filepath.Dir(tmpPath), 0777) // subject to umask
		if err != nil {
			return err
		}
		flags = flags | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(tmpPath, flags, 0666) // subject to umask
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, chunk)
	return err
}

// FinalizeBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) FinalizeBlob(ctx context.Context, account models.ReducedAccount, storageID string, chunkCount uint32) error {
	path := d.getBlobPath(account, storageID)
	tmpPath := path + ".tmp"
	return os.Rename(tmpPath, path)
}

// AbortBlobUpload implements the keppel.StorageDriver interface.
func (d *StorageDriver) AbortBlobUpload(ctx context.Context, account models.ReducedAccount, storageID string, chunkCount uint32) error {
	path := d.getBlobPath(account, storageID)
	tmpPath := path + ".tmp"
	return os.Remove(tmpPath)
}

// ReadBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) ReadBlob(ctx context.Context, account models.ReducedAccount, storageID string) (io.ReadCloser, uint64, error) {
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
	return f, keppel.AtLeastZero(stat.Size()), nil
}

// URLForBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) URLForBlob(ctx context.Context, account models.ReducedAccount, storageID string) (string, error) {
	return "", keppel.ErrCannotGenerateURL
}

// DeleteBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) DeleteBlob(ctx context.Context, account models.ReducedAccount, storageID string) error {
	path := d.getBlobPath(account, storageID)
	return os.Remove(path)
}

// ReadManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) ReadManifest(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest) ([]byte, error) {
	path := d.getManifestPath(account, repoName, manifestDigest)
	return os.ReadFile(path)
}

// WriteManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) WriteManifest(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, contents []byte) error {
	path := d.getManifestPath(account, repoName, manifestDigest)
	tmpPath := path + ".tmp"
	err := os.MkdirAll(filepath.Dir(tmpPath), 0777) // subject to umask
	if err != nil {
		return err
	}
	err = os.WriteFile(tmpPath, contents, 0666) // subject to umask
	if err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// DeleteManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) DeleteManifest(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest) error {
	path := d.getManifestPath(account, repoName, manifestDigest)
	return os.Remove(path)
}

// ReadTrivyReport implements the keppel.StorageDriver interface.
func (d *StorageDriver) ReadTrivyReport(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, format string) ([]byte, error) {
	path := d.getTrivyReportPath(account, repoName, manifestDigest, format)
	return os.ReadFile(path)
}

// WriteTrivyReport implements the keppel.StorageDriver interface.
func (d *StorageDriver) WriteTrivyReport(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, payload trivy.ReportPayload) error {
	path := d.getTrivyReportPath(account, repoName, manifestDigest, payload.Format)
	tmpPath := path + ".tmp"
	err := os.MkdirAll(filepath.Dir(tmpPath), 0777) // subject to umask
	if err != nil {
		return err
	}
	err = os.WriteFile(tmpPath, payload.Contents, 0666) // subject to umask
	if err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// DeleteTrivyReport implements the keppel.StorageDriver interface.
func (d *StorageDriver) DeleteTrivyReport(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, format string) error {
	path := d.getTrivyReportPath(account, repoName, manifestDigest, format)
	return os.Remove(path)
}

// ListStorageContents implements the keppel.StorageDriver interface.
func (d *StorageDriver) ListStorageContents(ctx context.Context, account models.ReducedAccount) ([]keppel.StoredBlobInfo, []keppel.StoredManifestInfo, []keppel.StoredTrivyReportInfo, error) {
	blobs, err := d.getBlobs(account)
	if err != nil {
		return nil, nil, nil, err
	}
	manifests, err := d.getManifests(account)
	if err != nil {
		return nil, nil, nil, err
	}
	trivyReports, err := d.getTrivyReports(account)
	if err != nil {
		return nil, nil, nil, err
	}
	return blobs, manifests, trivyReports, nil
}

func getNamesInDirectory(path string) ([]string, error) {
	directory, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer directory.Close()
	return directory.Readdirnames(-1)
}

func (d *StorageDriver) getBlobs(account models.ReducedAccount) ([]keppel.StoredBlobInfo, error) {
	names, err := getNamesInDirectory(d.getBlobBasePath(account))
	if err != nil {
		return nil, err
	}

	var blobs []keppel.StoredBlobInfo
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

func (d *StorageDriver) getManifests(account models.ReducedAccount) ([]keppel.StoredManifestInfo, error) {
	basePath := d.getManifestBasePath(account)
	repoNames, err := getNamesInDirectory(basePath)
	if err != nil {
		return nil, err
	}

	var manifests []keppel.StoredManifestInfo
	for _, repoName := range repoNames {
		names, err := getNamesInDirectory(filepath.Join(basePath, repoName))
		if err != nil {
			return nil, err
		}

		for _, name := range names {
			if strings.HasSuffix(name, ".tmp") {
				continue
			}

			manifestDigest, err := digest.Parse(name)
			if err != nil {
				return nil, fmt.Errorf("unexpected file in storage directory: %s", filepath.Join(basePath, repoName, name))
			}

			manifests = append(manifests, keppel.StoredManifestInfo{
				RepositoryName: repoName,
				Digest:         manifestDigest,
			})
		}
	}
	return manifests, nil
}

func (d *StorageDriver) getTrivyReports(account models.ReducedAccount) ([]keppel.StoredTrivyReportInfo, error) {
	basePath := d.getTrivyReportBasePath(account)
	repoNames, err := getNamesInDirectory(basePath)
	if err != nil {
		return nil, err
	}

	var reports []keppel.StoredTrivyReportInfo
	for _, repoName := range repoNames {
		digestStrings, err := getNamesInDirectory(filepath.Join(basePath, repoName))
		if err != nil {
			return nil, err
		}

		for _, digestStr := range digestStrings {
			manifestDigest, err := digest.Parse(digestStr)
			if err != nil {
				return nil, fmt.Errorf("unexpected file in storage directory: %s", filepath.Join(basePath, repoName, digestStr))
			}

			names, err := getNamesInDirectory(filepath.Join(basePath, repoName, digestStr))
			if err != nil {
				return nil, err
			}

			for _, name := range names {
				if strings.HasSuffix(name, ".tmp") {
					continue
				}

				reports = append(reports, keppel.StoredTrivyReportInfo{
					RepositoryName: repoName,
					Digest:         manifestDigest,
					Format:         name,
				})
			}
		}
	}
	return reports, nil
}

// CanSetupAccount implements the keppel.StorageDriver interface.
func (d *StorageDriver) CanSetupAccount(ctx context.Context, account models.ReducedAccount) error {
	return nil // this driver does not perform any preflight checks here
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
		report := storedTrivyReports[0]
		return fmt.Errorf(
			"found undeleted Trivy report during CleanupAccount: format=%q for %s@%s",
			report.Format,
			report.RepositoryName,
			report.Digest,
		)
	}
	return err
}
