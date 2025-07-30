// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"context"
	"database/sql"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/jobloop"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

var storageSweepSearchQuery = sqlext.SimplifyWhitespace(`
	SELECT * FROM accounts
		WHERE next_storage_sweep_at IS NULL OR next_storage_sweep_at < $1
	-- accounts without any sweeps first, then sorted by last sweep
	ORDER BY next_storage_sweep_at IS NULL DESC, next_storage_sweep_at ASC
	-- only one account at a time
	LIMIT 1
`)

var storageSweepDoneQuery = sqlext.SimplifyWhitespace(`
	UPDATE accounts SET next_storage_sweep_at = $2 WHERE name = $1
`)

// SweepStorageJob is a job. Each task finds an account where the backing storage
// needs to be garbage-collected, and performs the GC. This entails a marking of
// all blobs and manifests that exist in the backing storage, but not in the
// database; and a sweeping of all items that were marked in the previous pass
// and which are still not entered in the database.
//
// This staged mark-and-sweep ensures that we don't remove fresh blobs and
// manifests that were just pushed, but where the entry in the database is still
// being created.
//
// The storage of each account is sweeped at most once every 6 hours.
func (j *Janitor) StorageSweepJob(registerer prometheus.Registerer) jobloop.Job { //nolint:dupl // false positive
	return (&jobloop.ProducerConsumerJob[models.Account]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "storage sweep",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_storage_sweeps",
				Help: "Counter for garbage collections of an account's backing storage.",
			},
		},
		DiscoverTask: func(_ context.Context, _ prometheus.Labels) (account models.Account, err error) {
			err = j.db.SelectOne(&account, storageSweepSearchQuery, j.timeNow())
			return account, err
		},
		ProcessTask: j.sweepStorage,
	}).Setup(registerer)
}

// sweepStorage cleans up orphaned blobs, manifests and trivy reports from the storage driver.
//
// Note: This must be kept in synced with the slimmed down version in DeleteAccountsJob!
func (j *Janitor) sweepStorage(ctx context.Context, account models.Account, _ prometheus.Labels) error {
	reducedAccount := account.Reduced()

	// enumerate blobs, manifests and trivy reports in the backing storage
	actualBlobs, actualManifests, actualTrivyReports, err := j.sd.ListStorageContents(ctx, reducedAccount)
	if err != nil {
		return err
	}

	// when creating new entries in `unknown_blobs` and `unknown_manifests`, set
	// the `can_be_deleted_at` timestamp such that the next pass 6 hours from now
	// will sweep them (we don't use .Add(6 * time.Hour) to account for the
	// marking taking some time)
	canBeDeletedAt := j.timeNow().Add(4 * time.Hour)

	// handle blobs and manifests separately
	err = j.sweepBlobStorage(ctx, reducedAccount, actualBlobs, canBeDeletedAt)
	if err != nil {
		return err
	}
	err = j.sweepManifestStorage(ctx, reducedAccount, actualManifests, canBeDeletedAt)
	if err != nil {
		return err
	}
	err = j.sweepTrivyReportStorage(ctx, reducedAccount, actualTrivyReports, canBeDeletedAt)
	if err != nil {
		return err
	}

	_, err = j.db.Exec(storageSweepDoneQuery, account.Name, j.timeNow().Add(j.addJitter(6*time.Hour)))
	return err
}

// Note: The happy path to deleteUnknownBlob must be kept in sync with DeleteAccountsJob!
func (j *Janitor) sweepBlobStorage(ctx context.Context, account models.ReducedAccount, actualBlobs []keppel.StoredBlobInfo, canBeDeletedAt time.Time) error {
	actualBlobsByStorageID := make(map[string]keppel.StoredBlobInfo, len(actualBlobs))
	for _, blobInfo := range actualBlobs {
		actualBlobsByStorageID[blobInfo.StorageID] = blobInfo
	}

	// enumerate blobs known to the DB
	isKnownStorageID := make(map[string]bool)
	query := `SELECT storage_id FROM blobs WHERE account_name = $1`
	err := sqlext.ForeachRow(j.db, query, []any{account.Name}, func(rows *sql.Rows) error {
		var storageID string
		err := rows.Scan(&storageID)
		isKnownStorageID[storageID] = true
		return err
	})
	if err != nil {
		return err
	}

	// blobs in the backing storage may also correspond to uploads in progress
	query = `SELECT storage_id FROM uploads WHERE repo_id IN (SELECT id FROM repos WHERE account_name = $1)`
	err = sqlext.ForeachRow(j.db, query, []any{account.Name}, func(rows *sql.Rows) error {
		var storageID string
		err := rows.Scan(&storageID)
		isKnownStorageID[storageID] = true
		return err
	})
	if err != nil {
		return err
	}

	// unmark/sweep phase: enumerate all unknown blobs
	var unknownBlobs []models.UnknownBlob
	_, err = j.db.Select(&unknownBlobs, `SELECT * FROM unknown_blobs WHERE account_name = $1`, account.Name)
	if err != nil {
		return err
	}
	isMarkedStorageID := make(map[string]bool)
	for _, unknownBlob := range unknownBlobs {
		// unmark blobs that have been recorded in the database in the meantime
		if isKnownStorageID[unknownBlob.StorageID] {
			_, err = j.db.Delete(&unknownBlob)
			if err != nil {
				return err
			}
			continue
		}

		// sweep blobs that have been marked long enough
		isMarkedStorageID[unknownBlob.StorageID] = true
		if unknownBlob.CanBeDeletedAt.Before(j.timeNow()) {
			err = j.deleteUnknownBlob(ctx, actualBlobsByStorageID, account, unknownBlob)
			if err != nil {
				return err
			}
		}
	}

	// mark phase: record newly discovered unknown blobs in the DB
	for storageID := range actualBlobsByStorageID {
		if isKnownStorageID[storageID] || isMarkedStorageID[storageID] {
			continue
		}
		err := j.db.Insert(&models.UnknownBlob{
			AccountName:    account.Name,
			StorageID:      storageID,
			CanBeDeletedAt: canBeDeletedAt,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (j *Janitor) deleteUnknownBlob(ctx context.Context, blobsByStorageID map[string]keppel.StoredBlobInfo, account models.ReducedAccount, unknownBlob models.UnknownBlob) error {
	var err error

	// only call DeleteBlob if we can still see the blob in the backing
	// storage (this protects against unexpected errors e.g. because an
	// operator deleted the blob between the mark and sweep phases, or if we
	// deleted the blob from the backing storage in a previous sweep, but
	// could not remove the unknown_blobs entry from the DB)
	if blobInfo, exists := blobsByStorageID[unknownBlob.StorageID]; exists {
		// need to use different cleanup strategies depending on whether the
		// blob upload was finalized or not
		if blobInfo.ChunkCount > 0 {
			logg.Info("storage sweep in account %s: removing unfinalized blob stored at %s with %d chunks",
				account.Name, unknownBlob.StorageID, blobInfo.ChunkCount)
			err = j.sd.AbortBlobUpload(ctx, account, unknownBlob.StorageID, blobInfo.ChunkCount)
		} else {
			logg.Info("storage sweep in account %s: removing finalized blob stored at %s",
				account.Name, unknownBlob.StorageID)
			err = j.sd.DeleteBlob(ctx, account, unknownBlob.StorageID)
		}
		if err != nil {
			return err
		}
	}
	_, err = j.db.Delete(&unknownBlob)
	return err
}

// Note: The happy path to deleteUnknownManifest must be kept in sync with DeleteAccountsJob!
func (j *Janitor) sweepManifestStorage(ctx context.Context, account models.ReducedAccount, actualManifests []keppel.StoredManifestInfo, canBeDeletedAt time.Time) error {
	isActualManifest := make(map[keppel.StoredManifestInfo]bool, len(actualManifests))
	for _, m := range actualManifests {
		isActualManifest[m] = true
	}

	// enumerate manifests known to the DB
	isKnownManifest := make(map[keppel.StoredManifestInfo]bool)
	query := `SELECT r.name, m.digest FROM repos r JOIN manifests m ON m.repo_id = r.id WHERE r.account_name = $1`
	err := sqlext.ForeachRow(j.db, query, []any{account.Name}, func(rows *sql.Rows) error {
		var m keppel.StoredManifestInfo
		err := rows.Scan(&m.RepoName, &m.Digest)
		isKnownManifest[m] = true
		return err
	})
	if err != nil {
		return err
	}

	// unmark/sweep phase: enumerate all unknown manifests
	var unknownManifests []models.UnknownManifest
	_, err = j.db.Select(&unknownManifests, `SELECT * FROM unknown_manifests WHERE account_name = $1`, account.Name)
	if err != nil {
		return err
	}
	isMarkedManifest := make(map[keppel.StoredManifestInfo]bool)
	for _, unknownManifest := range unknownManifests {
		unknownManifestInfo := keppel.StoredManifestInfo{
			RepoName: unknownManifest.RepositoryName,
			Digest:   unknownManifest.Digest,
		}

		// unmark manifests that have been recorded in the database in the meantime
		if isKnownManifest[unknownManifestInfo] {
			_, err = j.db.Delete(&unknownManifest)
			if err != nil {
				return err
			}
			continue
		}

		// sweep manifests that have been marked long enough
		isMarkedManifest[unknownManifestInfo] = true
		if unknownManifest.CanBeDeletedAt.Before(j.timeNow()) {
			err = j.deleteUnknownManifest(ctx, isActualManifest, account, unknownManifest, unknownManifestInfo)
			if err != nil {
				return err
			}
		}
	}

	// mark phase: record newly discovered unknown manifests in the DB
	for manifest := range isActualManifest {
		if isKnownManifest[manifest] || isMarkedManifest[manifest] {
			continue
		}
		err := j.db.Insert(&models.UnknownManifest{
			AccountName:    account.Name,
			RepositoryName: manifest.RepoName,
			Digest:         manifest.Digest,
			CanBeDeletedAt: canBeDeletedAt,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (j *Janitor) deleteUnknownManifest(ctx context.Context, isActualManifest map[keppel.StoredManifestInfo]bool, account models.ReducedAccount, unknownManifest models.UnknownManifest, unknownManifestInfo keppel.StoredManifestInfo) error {
	var err error

	// only call DeleteManifest if we can still see the manifest in the
	// backing storage (this protects against unexpected errors e.g. because
	// an operator deleted the manifest between the mark and sweep phases, or
	// if we deleted the manifest from the backing storage in a previous
	// sweep, but could not remove the unknown_manifests entry from the DB)
	if isActualManifest[unknownManifestInfo] {
		logg.Info("storage sweep in account %s: removing manifest %s/%s",
			account.Name, unknownManifest.RepositoryName, unknownManifest.Digest)
		err := j.sd.DeleteManifest(ctx, account, unknownManifest.RepositoryName, unknownManifest.Digest)
		if err != nil {
			return err
		}
	}
	_, err = j.db.Delete(&unknownManifest)
	return err
}

// Note: The happy path to deleteUnknownTrivyReport must be kept in sync with DeleteAccountsJob!
func (j *Janitor) sweepTrivyReportStorage(ctx context.Context, account models.ReducedAccount, actualTrivyReports []keppel.StoredTrivyReportInfo, canBeDeletedAt time.Time) error {
	isActualReport := make(map[keppel.StoredTrivyReportInfo]bool, len(actualTrivyReports))
	for _, m := range actualTrivyReports {
		isActualReport[m] = true
	}

	// enumerate Trivy reports known to the DB
	isKnownReport := make(map[keppel.StoredTrivyReportInfo]bool)
	query := `SELECT r.name, t.digest FROM repos r JOIN trivy_security_info t ON t.repo_id = r.id WHERE r.account_name = $1 AND t.has_enriched_report`
	err := sqlext.ForeachRow(j.db, query, []any{account.Name}, func(rows *sql.Rows) error {
		r := keppel.StoredTrivyReportInfo{Format: "json"}
		err := rows.Scan(&r.RepoName, &r.Digest)
		isKnownReport[r] = true
		return err
	})
	if err != nil {
		return err
	}

	// unmark/sweep phase: enumerate all unknown Trivy reports
	var unknownReports []models.UnknownTrivyReport
	_, err = j.db.Select(&unknownReports, `SELECT * FROM unknown_trivy_reports WHERE account_name = $1`, account.Name)
	if err != nil {
		return err
	}
	isMarkedReport := make(map[keppel.StoredTrivyReportInfo]bool)
	for _, unknownReport := range unknownReports {
		unknownReportInfo := keppel.StoredTrivyReportInfo{
			RepoName: unknownReport.RepositoryName,
			Digest:   unknownReport.Digest,
			Format:   unknownReport.Format,
		}

		// unmark reports that have been recorded in the database in the meantime
		if isKnownReport[unknownReportInfo] {
			_, err = j.db.Delete(&unknownReport)
			if err != nil {
				return err
			}
			continue
		}

		// sweep reports that have been marked long enough
		isMarkedReport[unknownReportInfo] = true
		if unknownReport.CanBeDeletedAt.Before(j.timeNow()) {
			err = j.deleteUnknownTrivyReport(ctx, isActualReport, account, unknownReportInfo, unknownReport)
			if err != nil {
				return err
			}
		}
	}

	// mark phase: record newly discovered unknown reports in the DB
	for report := range isActualReport {
		if isKnownReport[report] || isMarkedReport[report] {
			continue
		}
		err := j.db.Insert(&models.UnknownTrivyReport{
			AccountName:    account.Name,
			RepositoryName: report.RepoName,
			Digest:         report.Digest,
			Format:         report.Format,
			CanBeDeletedAt: canBeDeletedAt,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (j *Janitor) deleteUnknownTrivyReport(ctx context.Context, isActualReport map[keppel.StoredTrivyReportInfo]bool, account models.ReducedAccount, unknownReportInfo keppel.StoredTrivyReportInfo, unknownReport models.UnknownTrivyReport) error {
	var err error

	// only call DeleteTrivyReport if we can still see the report in the
	// backing storage (this protects against unexpected errors e.g. because
	// an operator deleted the manifest between the mark and sweep phases, or
	// if we deleted the report from the backing storage in a previous sweep,
	// but could not remove the unknown_trivy_reports entry from the DB)
	if isActualReport[unknownReportInfo] {
		logg.Info("storage sweep in account %s: removing Trivy report %s/%s/%s",
			account.Name, unknownReport.RepositoryName, unknownReport.Digest, unknownReport.Format)
		err := j.sd.DeleteTrivyReport(ctx, account, unknownReport.RepositoryName, unknownReport.Digest, unknownReport.Format)
		if err != nil {
			return err
		}
	}
	_, err = j.db.Delete(&unknownReport)
	return err
}
