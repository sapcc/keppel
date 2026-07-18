// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package multi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/opencontainers/go-digest"
	. "go.xyrillian.de/gg/option"

	"github.com/sapcc/go-bits/logg"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/trivy"
)

type phase string

const (
	CopyPhase     phase = "copy"
	CleanupPhase  phase = "cleanup"
	FinalizePhase phase = "finalize"
)

// StorageDriver (driver ID "multi") is a keppel.StorageDriver for use to migrate from one storage backend to another.
type StorageDriver struct {
	OldParams json.RawMessage `json:"old"`
	NewParams json.RawMessage `json:"new"`
	Phase     phase           `json:"phase"`

	oldDriver keppel.StorageDriver
	newDriver keppel.StorageDriver
}

func init() {
	keppel.StorageDriverRegistry.Add(func() keppel.StorageDriver { return &StorageDriver{} })
}

// PluginTypeID implements the keppel.StorageDriver interface.
func (d *StorageDriver) PluginTypeID() string { return "multi" }

// Init implements the keppel.StorageDriver interface.
func (d *StorageDriver) Init(ctx context.Context, ad keppel.AuthDriver, cfg keppel.Configuration) error {
	if d.Phase != CopyPhase && d.Phase != CleanupPhase && d.Phase != FinalizePhase {
		return fmt.Errorf("phase contains invalid name %s, only copy, cleanup or finalize are allowed", d.Phase)
	}

	logg.Debug("initializing multi storage driver old driver: %q", d.OldParams)
	oldDriver, err := keppel.NewStorageDriver(ctx, string(d.OldParams), ad, cfg)
	if err != nil {
		return fmt.Errorf("while initializing old driver: %w", err)
	}
	d.oldDriver = oldDriver

	logg.Debug("initializing multi storage driver new driver: %q", d.NewParams)
	newDriver, err := keppel.NewStorageDriver(ctx, string(d.NewParams), ad, cfg)
	if err != nil {
		return fmt.Errorf("while initializing new driver: %w", err)
	}
	d.newDriver = newDriver

	return nil
}

// AppendToBlob implements the keppel.StorageDriver interface.
// In CopyPhase it appends to both storages in parallel.
func (d *StorageDriver) AppendToBlob(ctx context.Context, account models.ReducedAccount, storageID string, chunkNumber uint32, chunkLength Option[uint64], chunk io.Reader) error {
	switch d.Phase {
	case CopyPhase:
		pr, pw := io.Pipe()
		tee := io.TeeReader(chunk, pw)

		errCh := make(chan error, 1)

		go func() {
			defer pw.Close()
			defer close(errCh)
			err := d.oldDriver.AppendToBlob(ctx, account, storageID, chunkNumber, chunkLength, tee)
			if err != nil {
				err = fmt.Errorf("while calling old driver: %w", err)
				pw.CloseWithError(err)
			}
			errCh <- err
		}()

		err := d.newDriver.AppendToBlob(ctx, account, storageID, chunkNumber, chunkLength, pr)
		if err != nil {
			err = fmt.Errorf("while calling new driver: %w", err)
			pw.CloseWithError(err)
			<-errCh
			return err
		}

		return <-errCh
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// FinalizeBlob implements the keppel.StorageDriver interface.
// In CopyPhase it finalizes both blobs.
func (d *StorageDriver) FinalizeBlob(ctx context.Context, account models.ReducedAccount, storageID string, chunkCount uint32) error {
	switch d.Phase {
	case CopyPhase:
		err := d.oldDriver.FinalizeBlob(ctx, account, storageID, chunkCount)
		if err != nil {
			return fmt.Errorf("while finalizing blob in old driver: %w", err)
		}

		err = d.newDriver.FinalizeBlob(ctx, account, storageID, chunkCount)
		if err != nil {
			return fmt.Errorf("while finalizing blob in new driver: %w", err)
		}
		return nil
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// AbortBlobUpload implements the keppel.StorageDriver interface.
func (d *StorageDriver) AbortBlobUpload(ctx context.Context, account models.ReducedAccount, storageID string, chunkCount uint32) error {
	switch d.Phase {
	case CopyPhase:
		err := d.oldDriver.AbortBlobUpload(ctx, account, storageID, chunkCount)
		if err != nil {
			return fmt.Errorf("while aborting blob upload in old driver: %w", err)
		}

		err = d.newDriver.AbortBlobUpload(ctx, account, storageID, chunkCount)
		if err != nil {
			return fmt.Errorf("while aborting blob upload in new driver: %w", err)
		}

		return nil
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// ReadBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) ReadBlob(ctx context.Context, account models.ReducedAccount, storageID string) (io.ReadCloser, uint64, error) {
	switch d.Phase {
	case CopyPhase:
		return d.oldDriver.ReadBlob(ctx, account, storageID)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// appendToBlob contains the logic for splitting `contents` (containing `lengthBytes`) into chunks of `chunkSizeBytes` max.
// NOTE: This function is written such that `action` is called at least once, even when `contents` is empty.
func appendToBlob(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, storageID string, contents io.Reader, lengthBytes uint64, numChunks *uint32) error {
	var sizeBytes uint64

	action := func(chunk io.Reader, chunkLengthBytes uint64) error {
		*numChunks++
		sizeBytes += chunkLengthBytes
		return sd.AppendToBlob(ctx, account, storageID, *numChunks, Some(chunkLengthBytes), chunk)
	}

	remainingBytes := lengthBytes
	for remainingBytes > keppel.ChunkSizeBytes {
		err := action(io.LimitReader(contents, keppel.ChunkSizeBytes), keppel.ChunkSizeBytes)
		if err != nil {
			return err
		}
		remainingBytes -= keppel.ChunkSizeBytes
	}
	return action(contents, remainingBytes)
}

func (d *StorageDriver) migrateBlob(ctx context.Context, account models.ReducedAccount, storageID string) error {
	reader, sizeBytes, err := d.oldDriver.ReadBlob(ctx, account, storageID)
	if err != nil {
		return fmt.Errorf("while reading from old driver for replication: %w", err)
	}
	defer reader.Close()

	var numChunks uint32
	err = appendToBlob(ctx, d.newDriver, account, storageID, reader, sizeBytes, &numChunks)
	if err != nil {
		err = fmt.Errorf("while copying blob %s to new driver: %w", storageID, err)
		err2 := d.newDriver.AbortBlobUpload(ctx, account, storageID, numChunks)
		if err2 != nil {
			return errors.Join(err, fmt.Errorf("while aborting copying blob %s to new driver an additional error has occurred: %w", storageID, err2))
		}
		return err
	}

	err = d.newDriver.FinalizeBlob(ctx, account, storageID, numChunks)
	if err != nil {
		err = fmt.Errorf("while finalizing blob %s in new driver: %w", storageID, err)
		err2 := d.newDriver.AbortBlobUpload(ctx, account, storageID, numChunks)
		if err2 != nil {
			return errors.Join(err, fmt.Errorf("while aborting finalizing blob %s in new driver an additional error has occurred: %w", storageID, err2))
		}
		return err
	}

	return nil
}

// ReadBlobForValidation reads a blob for validation purposes.
// In CopyPhase it tries the new driver first. If the blob does not exist there,
// it reads from the old driver and simultaneously copies the data to the new driver
// so the blob is migrated on first validation.
func (d *StorageDriver) ReadBlobForValidation(ctx context.Context, account models.ReducedAccount, storageID string) (io.ReadCloser, uint64, error) {
	switch d.Phase {
	case CopyPhase:
		reader, sizeBytes, err := d.newDriver.ReadBlob(ctx, account, storageID)
		if errors.Is(err, os.ErrNotExist) {
			err = d.migrateBlob(ctx, account, storageID)
			if err != nil {
				// The replication failed, so we just validate the old blob below
				logg.Error(err.Error())
			}

			var (
				reader2    io.ReadCloser
				sizeBytes2 uint64
			)
			if err == nil {
				reader2, sizeBytes2, err = d.newDriver.ReadBlob(ctx, account, storageID)
				if err != nil {
					return nil, 0, fmt.Errorf("while reading from old driver: %w", err)
				}
			} else {
				reader2, sizeBytes2, err = d.oldDriver.ReadBlob(ctx, account, storageID)
				if err != nil {
					return nil, 0, fmt.Errorf("while reading from old driver: %w", err)
				}
			}

			return reader2, sizeBytes2, nil
		}

		if err != nil {
			return nil, 0, fmt.Errorf("while reading from new driver: %w", err)
		}

		return reader, sizeBytes, nil
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// URLForBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) URLForBlob(ctx context.Context, account models.ReducedAccount, storageID string) (string, error) {
	switch d.Phase {
	case CopyPhase:
		return d.oldDriver.URLForBlob(ctx, account, storageID)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// DeleteBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) DeleteBlob(ctx context.Context, account models.ReducedAccount, storageID string) error {
	switch d.Phase {
	case CopyPhase:
		oldErr := d.oldDriver.DeleteBlob(ctx, account, storageID)
		oldNotFound := errors.Is(oldErr, keppel.NotFoundInStorageError{})

		newErr := d.newDriver.DeleteBlob(ctx, account, storageID)
		newNotFound := errors.Is(newErr, keppel.NotFoundInStorageError{})

		if oldNotFound && newNotFound {
			return os.ErrNotExist
		}
		if oldErr != nil && !oldNotFound {
			return fmt.Errorf("while deleting from old driver: %w", oldErr)
		}
		if newErr != nil && !newNotFound {
			return fmt.Errorf("while deleting from new driver: %w", newErr)
		}
		return nil
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// ReadManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) ReadManifest(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest) ([]byte, error) {
	switch d.Phase {
	case CopyPhase:
		return d.oldDriver.ReadManifest(ctx, account, repoName, manifestDigest)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// ReadManifestForValidation implements the keppel.StorageDriver interface.
// In CopyPhase it tries the new driver first. If the manifest does not exist there,
// it reads from the old driver and copies it to the new driver
// so the manifest is migrated on first validation.
func (d *StorageDriver) ReadManifestForValidation(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest) ([]byte, error) {
	switch d.Phase {
	case CopyPhase:
		contents, err := d.newDriver.ReadManifest(ctx, account, repoName, manifestDigest)
		if errors.Is(err, os.ErrNotExist) {
			contents, err = d.oldDriver.ReadManifest(ctx, account, repoName, manifestDigest)
			if err != nil {
				return nil, fmt.Errorf("while reading from old driver: %w", err)
			}

			err := d.newDriver.WriteManifest(ctx, account, repoName, manifestDigest, contents)
			if err != nil {
				logg.Error("multi-driver: while copying manifest %s to new driver: %s", manifestDigest, err.Error())
			}

			return contents, nil
		}
		if err != nil {
			return nil, fmt.Errorf("while reading from new driver: %w", err)
		}
		return contents, nil
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// WriteManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) WriteManifest(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, contents []byte) error {
	switch d.Phase {
	case CopyPhase:
		err := d.oldDriver.WriteManifest(ctx, account, repoName, manifestDigest, contents)
		if err != nil {
			return fmt.Errorf("while writing to old driver: %w", err)
		}
		err = d.newDriver.WriteManifest(ctx, account, repoName, manifestDigest, contents)
		if err != nil {
			return fmt.Errorf("while writing to new driver: %w", err)
		}
		return nil
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// DeleteManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) DeleteManifest(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest) error {
	switch d.Phase {
	case CopyPhase:
		oldErr := d.oldDriver.DeleteManifest(ctx, account, repoName, manifestDigest)
		oldNotFound := errors.Is(oldErr, keppel.NotFoundInStorageError{})

		newErr := d.newDriver.DeleteManifest(ctx, account, repoName, manifestDigest)
		newNotFound := errors.Is(newErr, keppel.NotFoundInStorageError{})

		if oldNotFound && newNotFound {
			return os.ErrNotExist
		}
		if oldErr != nil && !oldNotFound {
			return fmt.Errorf("while deleting from old driver: %w", oldErr)
		}
		if newErr != nil && !newNotFound {
			return fmt.Errorf("while deleting from new driver: %w", newErr)
		}
		return nil
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// ReadTrivyReport implements the keppel.StorageDriver interface.
func (d *StorageDriver) ReadTrivyReport(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, format string) (io.ReadCloser, error) {
	switch d.Phase {
	case CopyPhase:
		return d.oldDriver.ReadTrivyReport(ctx, account, repoName, manifestDigest, format)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// WriteTrivyReport implements the keppel.StorageDriver interface.
func (d *StorageDriver) WriteTrivyReport(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, payload trivy.ReportPayload) error {
	switch d.Phase {
	case CopyPhase:
		err := d.oldDriver.WriteTrivyReport(ctx, account, repoName, manifestDigest, payload)
		if err != nil {
			return fmt.Errorf("while writing to old driver: %w", err)
		}
		err = d.newDriver.WriteTrivyReport(ctx, account, repoName, manifestDigest, payload)
		if err != nil {
			return fmt.Errorf("while writing to new driver: %w", err)
		}
		return nil
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// DeleteTrivyReport implements the keppel.StorageDriver interface.
func (d *StorageDriver) DeleteTrivyReport(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, format string) error {
	switch d.Phase {
	case CopyPhase:
		oldErr := d.oldDriver.DeleteTrivyReport(ctx, account, repoName, manifestDigest, format)
		oldNotFound := errors.Is(oldErr, keppel.NotFoundInStorageError{})

		newErr := d.newDriver.DeleteTrivyReport(ctx, account, repoName, manifestDigest, format)
		newNotFound := errors.Is(newErr, keppel.NotFoundInStorageError{})

		if oldNotFound && newNotFound {
			return os.ErrNotExist
		}
		if oldErr != nil && !oldNotFound {
			return fmt.Errorf("while deleting from old driver: %w", oldErr)
		}
		if newErr != nil && !newNotFound {
			return fmt.Errorf("while deleting from new driver: %w", newErr)
		}
		return nil
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// ListStorageContents implements the keppel.StorageDriver interface.
func (d *StorageDriver) ListStorageContents(ctx context.Context, account models.ReducedAccount) ([]keppel.StoredBlobInfo, []keppel.StoredManifestInfo, []keppel.StoredTrivyReportInfo, error) {
	switch d.Phase {
	case CopyPhase:
		return d.oldDriver.ListStorageContents(ctx, account)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// UsedBytes implements the keppel.StorageDriver interface.
func (d *StorageDriver) UsedBytes(ctx context.Context, authTenantID string) (usedBytes uint64, err error) {
	switch d.Phase {
	case CopyPhase:
		return d.oldDriver.UsedBytes(ctx, authTenantID)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// CanSetupAccount implements the keppel.StorageDriver interface.
func (d *StorageDriver) CanSetupAccount(ctx context.Context, account models.ReducedAccount) error {
	switch d.Phase {
	case CopyPhase:
		err := d.oldDriver.CanSetupAccount(ctx, account)
		if err != nil {
			return fmt.Errorf("while checking old driver: %w", err)
		}

		err = d.newDriver.CanSetupAccount(ctx, account)
		if err != nil {
			return fmt.Errorf("while checking new driver: %w", err)
		}

		return nil
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// CleanupAccount implements the keppel.StorageDriver interface.
func (d *StorageDriver) CleanupAccount(ctx context.Context, account models.ReducedAccount) error {
	switch d.Phase {
	case CopyPhase:
		return d.oldDriver.CleanupAccount(ctx, account)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}
