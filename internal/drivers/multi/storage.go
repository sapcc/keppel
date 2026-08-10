// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package multi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/logg"
	"go.xyrillian.de/gg/errext"
	. "go.xyrillian.de/gg/option"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/trivy"
)

type phase string

const (
	copyPhase     phase = "copy"
	cleanupPhase  phase = "cleanup"
	finalizePhase phase = "finalize"
)

// StorageDriver (driver ID "multi") is a keppel.StorageDriver for use to migrate from one storage backend to another.
type StorageDriver struct {
	OldParams json.RawMessage `json:"old"`
	NewParams json.RawMessage `json:"new"`
	Phase     phase           `json:"phase"`

	oldDriver keppel.StorageDriver
	newDriver keppel.StorageDriver
}

var (
	copiedBlobsCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "keppel_multi_storage_driver_copied_blobs",
			Help: "Counter for how many blobs were copied from the old storage driver to the new one.",
		},
	)
	copiedManifestsCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "keppel_multi_storage_driver_copied_manifests",
			Help: "Counter for how many manifests were copied from the old storage driver to the new one.",
		},
	)

	cleanedUpCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "keppel_multi_storage_driver_cleaned_up",
			Help: "Counter for how many objects were cleaned up from the old storage driver during cleanup phase.",
		},
		[]string{"type"},
	)

	objectsGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "keppel_multi_storage_driver_objects",
			Help: "Number of objects in the old and new storage driver by account and type.",
		},
		[]string{"account", "side", "type"},
	)
)

func init() {
	keppel.StorageDriverRegistry.Add(func() keppel.StorageDriver { return &StorageDriver{} })
	prometheus.MustRegister(copiedBlobsCounter)
	prometheus.MustRegister(copiedManifestsCounter)
	prometheus.MustRegister(cleanedUpCounter)
	prometheus.MustRegister(objectsGauge)
}

// PluginTypeID implements the keppel.StorageDriver interface.
func (d *StorageDriver) PluginTypeID() string { return "multi" }

// Init implements the keppel.StorageDriver interface.
func (d *StorageDriver) Init(ctx context.Context, ad keppel.AuthDriver, cfg keppel.Configuration) error {
	if d.Phase != copyPhase && d.Phase != cleanupPhase && d.Phase != finalizePhase {
		return fmt.Errorf("phase contains invalid name %s, only copy, cleanup or finalize are allowed", d.Phase)
	}

	oldDriver, err := keppel.NewStorageDriver(ctx, string(d.OldParams), ad, cfg)
	if err != nil {
		return fmt.Errorf("while initializing old driver: %w", err)
	}
	d.oldDriver = oldDriver

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
	case copyPhase:
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
	case cleanupPhase, finalizePhase:
		return d.newDriver.AppendToBlob(ctx, account, storageID, chunkNumber, chunkLength, chunk)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// FinalizeBlob implements the keppel.StorageDriver interface.
// In CopyPhase it finalizes both blobs.
func (d *StorageDriver) FinalizeBlob(ctx context.Context, account models.ReducedAccount, storageID string, chunkCount uint32) error {
	switch d.Phase {
	case copyPhase:
		err := d.oldDriver.FinalizeBlob(ctx, account, storageID, chunkCount)
		if err != nil {
			return fmt.Errorf("while finalizing blob in old driver: %w", err)
		}

		err = d.newDriver.FinalizeBlob(ctx, account, storageID, chunkCount)
		if err != nil {
			return fmt.Errorf("while finalizing blob in new driver: %w", err)
		}
		return nil
	case cleanupPhase, finalizePhase:
		return d.newDriver.FinalizeBlob(ctx, account, storageID, chunkCount)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// AbortBlobUpload implements the keppel.StorageDriver interface.
func (d *StorageDriver) AbortBlobUpload(ctx context.Context, account models.ReducedAccount, storageID string, chunkCount uint32) error {
	switch d.Phase {
	case copyPhase:
		err := d.oldDriver.AbortBlobUpload(ctx, account, storageID, chunkCount)
		if err != nil {
			return fmt.Errorf("while aborting blob upload in old driver: %w", err)
		}

		err = d.newDriver.AbortBlobUpload(ctx, account, storageID, chunkCount)
		if err != nil {
			return fmt.Errorf("while aborting blob upload in new driver: %w", err)
		}

		return nil
	case cleanupPhase, finalizePhase:
		return d.newDriver.AbortBlobUpload(ctx, account, storageID, chunkCount)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// ReadBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) ReadBlob(ctx context.Context, account models.ReducedAccount, storageID string) (io.ReadCloser, uint64, error) {
	switch d.Phase {
	case copyPhase:
		return d.oldDriver.ReadBlob(ctx, account, storageID)
	case cleanupPhase, finalizePhase:
		return d.newDriver.ReadBlob(ctx, account, storageID)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// appendToBlob contains the logic for splitting `contents` (containing `lengthBytes`) into chunks of `chunkSizeBytes` max.
// NOTE: This function is written such that `action` is called at least once, even when `contents` is empty.
func appendToBlob(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, storageID string, contents io.Reader, lengthBytes uint64, numChunks *uint32) error {
	remainingBytes := lengthBytes
	for remainingBytes > keppel.ChunkSizeBytes {
		*numChunks++
		err := sd.AppendToBlob(ctx, account, storageID, *numChunks, Some[uint64](keppel.ChunkSizeBytes), io.LimitReader(contents, keppel.ChunkSizeBytes))
		if err != nil {
			return err
		}
		remainingBytes -= keppel.ChunkSizeBytes
	}
	*numChunks++
	return sd.AppendToBlob(ctx, account, storageID, *numChunks, Some(remainingBytes), contents)
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
		return errext.WithCleanup(err, "newDriver.AbortBlobUpload", err2)
	}

	err = d.newDriver.FinalizeBlob(ctx, account, storageID, numChunks)
	if err != nil {
		err = fmt.Errorf("while finalizing blob %s in new driver: %w", storageID, err)
		err2 := d.newDriver.AbortBlobUpload(ctx, account, storageID, numChunks)
		return errext.WithCleanup(err, "newDriver.AbortBlobUpload", err2)
	}

	copiedBlobsCounter.Inc()

	return nil
}

// ReadBlobForValidation reads a blob for validation purposes.
// In CopyPhase it tries the new driver first. If the blob does not exist there,
// it reads from the old driver and copies the data to the new driver so the blob is migrated on first validation.
func (d *StorageDriver) ReadBlobForValidation(ctx context.Context, account models.ReducedAccount, storageID string) (io.ReadCloser, uint64, error) {
	switch d.Phase {
	case copyPhase:
		reader, sizeBytes, err := d.newDriver.ReadBlob(ctx, account, storageID)
		if errors.Is(err, keppel.NotFoundInStorageError{}) {
			migrateErr := d.migrateBlob(ctx, account, storageID)
			if migrateErr != nil {
				// The replication failed, so we just validate the old blob below
				logg.Error(migrateErr.Error())
			}

			if migrateErr == nil {
				reader, sizeBytes, err = d.newDriver.ReadBlobForValidation(ctx, account, storageID)
				if err != nil {
					return nil, 0, fmt.Errorf("while reading from new driver: %w", err)
				}
			} else {
				reader, sizeBytes, err = d.oldDriver.ReadBlobForValidation(ctx, account, storageID)
				if err != nil {
					return nil, 0, fmt.Errorf("while reading from old driver: %w", err)
				}
			}

			return reader, sizeBytes, nil
		}

		if err != nil {
			return nil, 0, fmt.Errorf("while reading from new driver: %w", err)
		}

		return reader, sizeBytes, nil
	case cleanupPhase:
		reader, size, err := d.newDriver.ReadBlobForValidation(ctx, account, storageID)
		if err != nil {
			return nil, 0, err
		}

		// delete blob only one we know it exists on the new side
		errOnOldSide := d.oldDriver.DeleteBlob(ctx, account, storageID)
		if errOnOldSide != nil && !errors.Is(errOnOldSide, keppel.NotFoundInStorageError{}) {
			return nil, 0, errext.WithCleanup(errOnOldSide, "Reader.Close", reader.Close())
		}
		if errOnOldSide == nil {
			cleanedUpCounter.With(prometheus.Labels{"type": "blobs"}).Inc()
		}

		return reader, size, err
	case finalizePhase:
		return d.newDriver.ReadBlobForValidation(ctx, account, storageID)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// URLForBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) URLForBlob(ctx context.Context, account models.ReducedAccount, storageID string) (string, error) {
	switch d.Phase {
	case copyPhase:
		return d.oldDriver.URLForBlob(ctx, account, storageID)
	case cleanupPhase, finalizePhase:
		return d.newDriver.URLForBlob(ctx, account, storageID)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// DeleteBlob implements the keppel.StorageDriver interface.
func (d *StorageDriver) DeleteBlob(ctx context.Context, account models.ReducedAccount, storageID string) error {
	switch d.Phase {
	case copyPhase:
		oldErr := d.oldDriver.DeleteBlob(ctx, account, storageID)
		oldNotFound := errors.Is(oldErr, keppel.NotFoundInStorageError{})

		newErr := d.newDriver.DeleteBlob(ctx, account, storageID)
		newNotFound := errors.Is(newErr, keppel.NotFoundInStorageError{})

		if oldNotFound && newNotFound {
			return fmt.Errorf("missing in both drivers: old driver: %w, new driver: %w", oldErr, newErr)
		}
		if oldErr != nil && !oldNotFound {
			return fmt.Errorf("while deleting from old driver: %w", oldErr)
		}
		if newErr != nil && !newNotFound {
			return fmt.Errorf("while deleting from new driver: %w", newErr)
		}
		return nil
	case cleanupPhase:
		err := d.oldDriver.DeleteBlob(ctx, account, storageID)
		if err != nil && !errors.Is(err, keppel.NotFoundInStorageError{}) {
			return err
		}
		if err == nil {
			cleanedUpCounter.With(prometheus.Labels{"type": "blobs"}).Inc()
		}

		return d.newDriver.DeleteBlob(ctx, account, storageID)
	case finalizePhase:
		return d.newDriver.DeleteBlob(ctx, account, storageID)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// ReadManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) ReadManifest(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest) ([]byte, error) {
	switch d.Phase {
	case copyPhase:
		return d.oldDriver.ReadManifest(ctx, account, repoName, manifestDigest)
	case cleanupPhase, finalizePhase:
		return d.newDriver.ReadManifest(ctx, account, repoName, manifestDigest)
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
	case copyPhase:
		contents, err := d.newDriver.ReadManifestForValidation(ctx, account, repoName, manifestDigest)
		if errors.Is(err, keppel.NotFoundInStorageError{}) {
			contents, err = d.oldDriver.ReadManifestForValidation(ctx, account, repoName, manifestDigest)
			if err != nil {
				return nil, fmt.Errorf("while reading from old driver: %w", err)
			}

			err := d.newDriver.WriteManifest(ctx, account, repoName, manifestDigest, contents)
			if err == nil {
				copiedManifestsCounter.Inc()
			} else {
				logg.Error("multi-driver: while copying manifest %s to new driver: %s", manifestDigest, err.Error())
			}

			return contents, nil
		}
		if err != nil {
			return nil, fmt.Errorf("while reading from new driver: %w", err)
		}
		return contents, nil
	case cleanupPhase:
		manifest, err := d.newDriver.ReadManifest(ctx, account, repoName, manifestDigest)
		if err != nil {
			return nil, err
		}

		// delete manifest only one we know it exists on the new side
		errOnOldSide := d.oldDriver.DeleteManifest(ctx, account, repoName, manifestDigest)
		if errOnOldSide != nil && !errors.Is(errOnOldSide, keppel.NotFoundInStorageError{}) {
			return nil, errOnOldSide
		}
		if errOnOldSide == nil {
			cleanedUpCounter.With(prometheus.Labels{"type": "manifests"}).Inc()
		}

		return manifest, err
	case finalizePhase:
		return d.newDriver.ReadManifest(ctx, account, repoName, manifestDigest)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// WriteManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) WriteManifest(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, contents []byte) error {
	switch d.Phase {
	case copyPhase:
		err := d.oldDriver.WriteManifest(ctx, account, repoName, manifestDigest, contents)
		if err != nil {
			return fmt.Errorf("while writing to old driver: %w", err)
		}

		err = d.newDriver.WriteManifest(ctx, account, repoName, manifestDigest, contents)
		if err != nil {
			return fmt.Errorf("while writing to new driver: %w", err)
		}

		return nil
	case cleanupPhase:
		err := d.oldDriver.DeleteManifest(ctx, account, repoName, manifestDigest)
		if err != nil && !errors.Is(err, keppel.NotFoundInStorageError{}) {
			return err
		}
		if err == nil {
			cleanedUpCounter.With(prometheus.Labels{"type": "manifests"}).Inc()
		}

		return d.newDriver.WriteManifest(ctx, account, repoName, manifestDigest, contents)
	case finalizePhase:
		return d.newDriver.WriteManifest(ctx, account, repoName, manifestDigest, contents)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// DeleteManifest implements the keppel.StorageDriver interface.
func (d *StorageDriver) DeleteManifest(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest) error {
	switch d.Phase {
	case copyPhase:
		oldErr := d.oldDriver.DeleteManifest(ctx, account, repoName, manifestDigest)
		oldNotFound := errors.Is(oldErr, keppel.NotFoundInStorageError{})

		newErr := d.newDriver.DeleteManifest(ctx, account, repoName, manifestDigest)
		newNotFound := errors.Is(newErr, keppel.NotFoundInStorageError{})

		if oldNotFound && newNotFound {
			return fmt.Errorf("missing in both drivers: old driver: %w, new driver: %w", oldErr, newErr)
		}
		if oldErr != nil && !oldNotFound {
			return fmt.Errorf("while deleting from old driver: %w", oldErr)
		}
		if newErr != nil && !newNotFound {
			return fmt.Errorf("while deleting from new driver: %w", newErr)
		}
		return nil
	case cleanupPhase:
		err := d.oldDriver.DeleteManifest(ctx, account, repoName, manifestDigest)
		if err != nil && !errors.Is(err, keppel.NotFoundInStorageError{}) {
			return err
		}
		if err == nil {
			cleanedUpCounter.With(prometheus.Labels{"type": "manifests"}).Inc()
		}

		return d.newDriver.DeleteManifest(ctx, account, repoName, manifestDigest)
	case finalizePhase:
		return d.newDriver.DeleteManifest(ctx, account, repoName, manifestDigest)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// ReadTrivyReport implements the keppel.StorageDriver interface.
func (d *StorageDriver) ReadTrivyReport(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, format string) (io.ReadCloser, error) {
	switch d.Phase {
	case copyPhase:
		return d.oldDriver.ReadTrivyReport(ctx, account, repoName, manifestDigest, format)
	case cleanupPhase, finalizePhase:
		return d.newDriver.ReadTrivyReport(ctx, account, repoName, manifestDigest, format)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// WriteTrivyReport implements the keppel.StorageDriver interface.
func (d *StorageDriver) WriteTrivyReport(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, payload trivy.ReportPayload) error {
	switch d.Phase {
	case copyPhase:
		err := d.oldDriver.WriteTrivyReport(ctx, account, repoName, manifestDigest, payload)
		if err != nil {
			return fmt.Errorf("while writing to old driver: %w", err)
		}
		err = d.newDriver.WriteTrivyReport(ctx, account, repoName, manifestDigest, payload)
		if err != nil {
			return fmt.Errorf("while writing to new driver: %w", err)
		}
		return nil
	case cleanupPhase:
		err := d.oldDriver.DeleteTrivyReport(ctx, account, repoName, manifestDigest, payload.Format)
		if err != nil && !errors.Is(err, keppel.NotFoundInStorageError{}) {
			return err
		}
		if err == nil {
			cleanedUpCounter.With(prometheus.Labels{"type": "trivy_reports"}).Inc()
		}

		return d.newDriver.WriteTrivyReport(ctx, account, repoName, manifestDigest, payload)
	case finalizePhase:
		return d.newDriver.WriteTrivyReport(ctx, account, repoName, manifestDigest, payload)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// DeleteTrivyReport implements the keppel.StorageDriver interface.
func (d *StorageDriver) DeleteTrivyReport(ctx context.Context, account models.ReducedAccount, repoName string, manifestDigest digest.Digest, format string) error {
	switch d.Phase {
	case copyPhase:
		oldErr := d.oldDriver.DeleteTrivyReport(ctx, account, repoName, manifestDigest, format)
		oldNotFound := errors.Is(oldErr, keppel.NotFoundInStorageError{})

		newErr := d.newDriver.DeleteTrivyReport(ctx, account, repoName, manifestDigest, format)
		newNotFound := errors.Is(newErr, keppel.NotFoundInStorageError{})

		if oldNotFound && newNotFound {
			return fmt.Errorf("missing in both drivers: old driver: %w, new driver: %w", oldErr, newErr)
		}
		if oldErr != nil && !oldNotFound {
			return fmt.Errorf("while deleting from old driver: %w", oldErr)
		}
		if newErr != nil && !newNotFound {
			return fmt.Errorf("while deleting from new driver: %w", newErr)
		}
		return nil
	case cleanupPhase:
		err := d.oldDriver.DeleteTrivyReport(ctx, account, repoName, manifestDigest, format)
		if err != nil && !errors.Is(err, keppel.NotFoundInStorageError{}) {
			return err
		}
		if err == nil {
			cleanedUpCounter.With(prometheus.Labels{"type": "trivy_reports"}).Inc()
		}

		return d.newDriver.DeleteTrivyReport(ctx, account, repoName, manifestDigest, format)
	case finalizePhase:
		return d.newDriver.DeleteTrivyReport(ctx, account, repoName, manifestDigest, format)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// ListStorageContents implements the keppel.StorageDriver interface.
func (d *StorageDriver) ListStorageContents(ctx context.Context, account models.ReducedAccount) ([]keppel.StoredBlobInfo, []keppel.StoredManifestInfo, []keppel.StoredTrivyReportInfo, error) {
	switch d.Phase {
	case copyPhase, cleanupPhase:
		oldBlobs, oldManifests, oldTrivyReports, err := d.oldDriver.ListStorageContents(ctx, account)
		if err != nil {
			return nil, nil, nil, err
		}
		newBlobs, newManifests, newTrivyReports, err := d.newDriver.ListStorageContents(ctx, account)
		if err != nil {
			return nil, nil, nil, err
		}

		objectsGauge.With(prometheus.Labels{"account": string(account.Name), "side": "old", "type": "blobs"}).Set(float64(len(oldBlobs)))
		objectsGauge.With(prometheus.Labels{"account": string(account.Name), "side": "old", "type": "manifests"}).Set(float64(len(oldManifests)))
		objectsGauge.With(prometheus.Labels{"account": string(account.Name), "side": "old", "type": "trivy_reports"}).Set(float64(len(oldTrivyReports)))
		objectsGauge.With(prometheus.Labels{"account": string(account.Name), "side": "new", "type": "blobs"}).Set(float64(len(newBlobs)))
		objectsGauge.With(prometheus.Labels{"account": string(account.Name), "side": "new", "type": "manifests"}).Set(float64(len(newManifests)))
		objectsGauge.With(prometheus.Labels{"account": string(account.Name), "side": "new", "type": "trivy_reports"}).Set(float64(len(newTrivyReports)))

		if d.Phase == copyPhase {
			return oldBlobs, oldManifests, oldTrivyReports, nil
		} else {
			return newBlobs, newManifests, newTrivyReports, nil
		}
	case finalizePhase:
		err := d.oldDriver.CleanupAccount(ctx, account)
		if err != nil && !errors.Is(err, keppel.NotFoundInStorageError{}) {
			return nil, nil, nil, fmt.Errorf("cannot clean up account %q in old storage driver: %w", account.Name, err)
		}

		return d.newDriver.ListStorageContents(ctx, account)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// UsedBytes implements the keppel.StorageDriver interface.
func (d *StorageDriver) UsedBytes(ctx context.Context, authTenantID string) (usedBytes uint64, err error) {
	switch d.Phase {
	case copyPhase:
		return d.oldDriver.UsedBytes(ctx, authTenantID)
	case cleanupPhase, finalizePhase:
		return d.newDriver.UsedBytes(ctx, authTenantID)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// CanSetupAccount implements the keppel.StorageDriver interface.
func (d *StorageDriver) CanSetupAccount(ctx context.Context, account models.ReducedAccount) error {
	switch d.Phase {
	case copyPhase:
		err := d.oldDriver.CanSetupAccount(ctx, account)
		if err != nil {
			return fmt.Errorf("while checking old driver: %w", err)
		}

		err = d.newDriver.CanSetupAccount(ctx, account)
		if err != nil {
			return fmt.Errorf("while checking new driver: %w", err)
		}

		return nil
	case cleanupPhase, finalizePhase:
		return d.newDriver.CanSetupAccount(ctx, account)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}

// CleanupAccount implements the keppel.StorageDriver interface.
func (d *StorageDriver) CleanupAccount(ctx context.Context, account models.ReducedAccount) error {
	switch d.Phase {
	case copyPhase:
		err := d.oldDriver.CleanupAccount(ctx, account)
		if err != nil {
			return fmt.Errorf("while cleaning up old driver: %w", err)
		}

		err = d.newDriver.CleanupAccount(ctx, account)
		if err != nil {
			return fmt.Errorf("while cleaning up new driver: %w", err)
		}

		return nil
	case cleanupPhase, finalizePhase:
		return d.newDriver.CleanupAccount(ctx, account)
	default:
		panic(fmt.Sprintf("multi-driver: unexpected phase %q", d.Phase))
	}
}
