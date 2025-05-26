// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"bytes"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/jobloop"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
	"github.com/sapcc/keppel/internal/trivy"
)

func setupStorageSweepTest(t *testing.T, s test.Setup, sweepStorageJob jobloop.Job) (tr *easypg.Tracker, images []test.Image, healthyBlobs []models.Blob, healthyManifests []models.Manifest, healthyTrivyReports map[models.Manifest][]trivy.ReportPayload) {
	// setup some manifests, blobs and Trivy reports as a baseline;
	// StorageSweepJob should never touch these
	images = make([]test.Image, 2)
	healthyTrivyReports = make(map[models.Manifest][]trivy.ReportPayload)
	for idx := range images {
		image := test.GenerateImage(
			test.GenerateExampleLayer(int64(10*idx+1)),
			test.GenerateExampleLayer(int64(10*idx+2)),
		)
		images[idx] = image

		healthyBlobs = append(healthyBlobs,
			image.Layers[0].MustUpload(t, s, fooRepoRef),
			image.Layers[1].MustUpload(t, s, fooRepoRef),
			image.Config.MustUpload(t, s, fooRepoRef),
		)
		manifest := image.MustUpload(t, s, fooRepoRef, "")
		healthyManifests = append(healthyManifests, manifest)

		// we do not have a .MustUpload() helper for Trivy reports because there is no API for uploading them;
		// we need to write them into storage and also set the respective field to mark their existence in the DB
		dummyReport := mustUploadDummyTrivyReport(t, s, manifest)
		healthyTrivyReports[manifest] = append(healthyTrivyReports[manifest], dummyReport)
		mustExec(t, s.DB,
			"UPDATE trivy_security_info SET vuln_status = $1, has_enriched_report = TRUE WHERE digest = $2",
			models.CleanSeverity, manifest.Digest.String(),
		)
	}

	imageList := test.GenerateImageList(images[0], images[1])
	healthyManifests = append(healthyManifests,
		imageList.MustUpload(t, s, fooRepoRef, ""),
	)

	// StorageSweepJob should run through, but not do anything besides
	// setting the storage_sweeped_at timestamp on the account
	expectSuccess(t, sweepStorageJob.ProcessOne(s.Ctx))
	expectError(t, sql.ErrNoRows.Error(), sweepStorageJob.ProcessOne(s.Ctx))
	tr, tr0 := easypg.NewTracker(t, s.DB.Db)
	tr0.AssertEqualToFile("fixtures/storage-sweep-000.sql")
	s.ExpectBlobsExistInStorage(t, healthyBlobs...)
	s.ExpectManifestsExistInStorage(t, "foo", healthyManifests...)
	for manifest, reports := range healthyTrivyReports {
		for _, report := range reports {
			s.ExpectTrivyReportExistsInStorage(t, manifest, report.Format, assert.ByteData(report.Contents))
		}
	}

	return tr, images, healthyBlobs, healthyManifests, healthyTrivyReports
}

func mustUploadDummyTrivyReport(t *testing.T, s test.Setup, manifest models.Manifest) trivy.ReportPayload {
	t.Helper()
	report := trivy.ReportPayload{
		Format:   "json",
		Contents: []byte(fmt.Sprintf(`{"dummy":"image %s is clean"}`, manifest.Digest.String())),
	}
	repo, err := keppel.FindRepositoryByID(s.DB, manifest.RepositoryID)
	mustDo(t, err)
	mustDo(t, s.SD.WriteTrivyReport(s.Ctx, models.ReducedAccount{Name: repo.AccountName}, repo.Name, manifest.Digest, report))
	return report
}

func TestSweepStorageBlobs(t *testing.T) {
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)
	sweepStorageJob := j.StorageSweepJob(s.Registry)
	tr, _, healthyBlobs, healthyManifests, healthyTrivyReports := setupStorageSweepTest(t, s, sweepStorageJob)

	// put some blobs in the storage without adding them in the DB
	account := models.ReducedAccount{Name: "test1"}
	testBlob1 := test.GenerateExampleLayer(30)
	testBlob2 := test.GenerateExampleLayer(31)
	storageID1 := testBlob1.Digest.Encoded()
	storageID2 := testBlob2.Digest.Encoded()
	for _, blob := range []test.Bytes{testBlob1, testBlob2} {
		storageID := blob.Digest.Encoded()
		sizeBytes := uint64(len(blob.Contents))
		test.MustDo(t, s.SD.AppendToBlob(s.Ctx, account, storageID, 1, &sizeBytes, bytes.NewReader(blob.Contents)))
		test.MustDo(t, s.SD.FinalizeBlob(s.Ctx, account, storageID, 1))
	}

	// create a blob that's mid-upload; this one should be protected from sweeping
	// by the presence of the Upload object in the DB
	testBlob3 := test.GenerateExampleLayer(32)
	storageID3 := testBlob3.Digest.Encoded()
	sizeBytes := uint64(len(testBlob3.Contents))
	test.MustDo(t, s.SD.AppendToBlob(s.Ctx, account, storageID3, 1, &sizeBytes, bytes.NewReader(testBlob3.Contents)))
	// ^ but no FinalizeBlob() since we're still uploading!
	test.MustDo(t, s.DB.Insert(&models.Upload{
		RepositoryID: 1,
		UUID:         "a29d525c-2273-44ba-83a8-eafd447f1cb8", // chosen at random, but fixed
		StorageID:    storageID3,
		SizeBytes:    sizeBytes,
		Digest:       testBlob3.Digest.String(),
		NumChunks:    1,
		UpdatedAt:    j.timeNow(),
	}))
	tr.DBChanges().Ignore()

	// create another blob that's mid-upload; this one will be sweeped later to
	// verify that we clean up unfinished uploads correctly
	testBlob4 := test.GenerateExampleLayer(33)
	storageID4 := testBlob4.Digest.Encoded()
	sizeBytes = uint64(len(testBlob4.Contents))
	test.MustDo(t, s.SD.AppendToBlob(s.Ctx, account, storageID4, 1, &sizeBytes, bytes.NewReader(testBlob4.Contents)))

	// next StorageSweepJob should mark them for deletion...
	s.Clock.StepBy(8 * time.Hour)
	expectSuccess(t, sweepStorageJob.ProcessOne(s.Ctx))
	expectError(t, sql.ErrNoRows.Error(), sweepStorageJob.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET next_storage_sweep_at = %[1]d WHERE name = 'test1';
			INSERT INTO unknown_blobs (account_name, storage_id, can_be_deleted_at) VALUES ('test1', '%[3]s', %[2]d);
			INSERT INTO unknown_blobs (account_name, storage_id, can_be_deleted_at) VALUES ('test1', '%[4]s', %[2]d);
			INSERT INTO unknown_blobs (account_name, storage_id, can_be_deleted_at) VALUES ('test1', '%[5]s', %[2]d);
		`,
		s.Clock.Now().Add(6*time.Hour).Unix(), // next_storage_sweep_at
		s.Clock.Now().Add(4*time.Hour).Unix(), // can_be_deleted_at
		storageID2, storageID1, storageID4,
	)

	// ...but not delete anything yet
	s.ExpectBlobsExistInStorage(t, healthyBlobs...)
	s.ExpectBlobsExistInStorage(t,
		models.Blob{AccountName: "test1", Digest: testBlob1.Digest, StorageID: testBlob1.Digest.Encoded()},
		models.Blob{AccountName: "test1", Digest: testBlob2.Digest, StorageID: testBlob2.Digest.Encoded()},
		models.Blob{AccountName: "test1", Digest: testBlob3.Digest, StorageID: testBlob3.Digest.Encoded()},
		models.Blob{AccountName: "test1", Digest: testBlob4.Digest, StorageID: testBlob4.Digest.Encoded()},
	)
	s.ExpectManifestsExistInStorage(t, "foo", healthyManifests...)
	for manifest, reports := range healthyTrivyReports {
		for _, report := range reports {
			s.ExpectTrivyReportExistsInStorage(t, manifest, report.Format, assert.ByteData(report.Contents))
		}
	}

	// create a DB entry for the first blob (to sort of simulate an upload that
	// just got finished while StorageSweepJob was running: blob was
	// written to storage already, but not yet to DB)
	s.Clock.StepBy(1 * time.Hour)
	dbTestBlob1 := models.Blob{
		AccountName:      "test1",
		Digest:           testBlob1.Digest,
		SizeBytes:        uint64(len(testBlob1.Contents)),
		StorageID:        testBlob1.Digest.Encoded(),
		PushedAt:         s.Clock.Now(),
		NextValidationAt: s.Clock.Now().Add(models.BlobValidationInterval),
	}
	test.MustDo(t, s.DB.Insert(&dbTestBlob1))
	tr.DBChanges().Ignore()

	// next StorageSweepJob should unmark blob 1 (because it's now in
	// the DB) and sweep blobs 2 and 4 (since it is still not in the DB)
	s.Clock.StepBy(8 * time.Hour)
	expectSuccess(t, sweepStorageJob.ProcessOne(s.Ctx))
	expectError(t, sql.ErrNoRows.Error(), sweepStorageJob.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET next_storage_sweep_at = %[1]d WHERE name = 'test1';
			DELETE FROM unknown_blobs WHERE account_name = 'test1' AND storage_id = '%[2]s';
			DELETE FROM unknown_blobs WHERE account_name = 'test1' AND storage_id = '%[3]s';
			DELETE FROM unknown_blobs WHERE account_name = 'test1' AND storage_id = '%[4]s';
		`,
		s.Clock.Now().Add(6*time.Hour).Unix(),
		storageID2, storageID1, storageID4,
	)
	s.ExpectBlobsExistInStorage(t, healthyBlobs...)
	s.ExpectBlobsExistInStorage(t,
		models.Blob{AccountName: "test1", Digest: testBlob1.Digest, StorageID: testBlob1.Digest.Encoded()},
		models.Blob{AccountName: "test1", Digest: testBlob3.Digest, StorageID: testBlob3.Digest.Encoded()},
	)
	s.ExpectBlobsMissingInStorage(t,
		models.Blob{AccountName: "test1", Digest: testBlob2.Digest, StorageID: testBlob2.Digest.Encoded()},
		models.Blob{AccountName: "test1", Digest: testBlob4.Digest, StorageID: testBlob4.Digest.Encoded()},
	)
	s.ExpectManifestsExistInStorage(t, "foo", healthyManifests...)
	for manifest, reports := range healthyTrivyReports {
		for _, report := range reports {
			s.ExpectTrivyReportExistsInStorage(t, manifest, report.Format, assert.ByteData(report.Contents))
		}
	}
}

func TestSweepStorageManifests(t *testing.T) {
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)
	sweepStorageJob := j.StorageSweepJob(s.Registry)
	tr, images, healthyBlobs, healthyManifests, healthyTrivyReports := setupStorageSweepTest(t, s, sweepStorageJob)

	// put some manifests in the storage without adding them in the DB
	account := models.ReducedAccount{Name: "test1"}
	testImageList1 := test.GenerateImageList(images[0])
	testImageList2 := test.GenerateImageList(images[1])
	for _, manifest := range []test.Bytes{testImageList1.Manifest, testImageList2.Manifest} {
		test.MustDo(t, s.SD.WriteManifest(s.Ctx, account, "foo", manifest.Digest, manifest.Contents))
	}

	// next StorageSweepJob should mark them for deletion...
	s.Clock.StepBy(8 * time.Hour)
	expectSuccess(t, sweepStorageJob.ProcessOne(s.Ctx))
	expectError(t, sql.ErrNoRows.Error(), sweepStorageJob.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET next_storage_sweep_at = %[1]d WHERE name = 'test1';
			INSERT INTO unknown_manifests (account_name, repo_name, digest, can_be_deleted_at) VALUES ('test1', 'foo', '%[3]s', %[2]d);
			INSERT INTO unknown_manifests (account_name, repo_name, digest, can_be_deleted_at) VALUES ('test1', 'foo', '%[4]s', %[2]d);
		`,
		s.Clock.Now().Add(6*time.Hour).Unix(), // next_storage_sweep_at
		s.Clock.Now().Add(4*time.Hour).Unix(), // can_be_deleted_at
		testImageList1.Manifest.Digest.String(),
		testImageList2.Manifest.Digest.String(),
	)

	// ...but not delete anything yet
	s.ExpectBlobsExistInStorage(t, healthyBlobs...)
	s.ExpectManifestsExistInStorage(t, "foo", healthyManifests...)
	s.ExpectManifestsExistInStorage(t, "foo",
		models.Manifest{RepositoryID: 1, Digest: testImageList1.Manifest.Digest},
		models.Manifest{RepositoryID: 1, Digest: testImageList2.Manifest.Digest},
	)
	for manifest, reports := range healthyTrivyReports {
		for _, report := range reports {
			s.ExpectTrivyReportExistsInStorage(t, manifest, report.Format, assert.ByteData(report.Contents))
		}
	}

	// create a DB entry for the first manifest (to sort of simulate a manifest
	// upload that happened while StorageSweepJob was running: manifest was written
	// to storage already, but not yet to DB)
	s.Clock.StepBy(1 * time.Hour)
	test.MustDo(t, s.DB.Insert(&models.Manifest{
		RepositoryID:     1,
		Digest:           testImageList1.Manifest.Digest,
		MediaType:        testImageList1.Manifest.MediaType,
		SizeBytes:        uint64(len(testImageList1.Manifest.Contents)),
		PushedAt:         s.Clock.Now(),
		NextValidationAt: s.Clock.Now().Add(models.ManifestValidationInterval),
	}))
	tr.DBChanges().Ignore()

	// next StorageSweepJob should unmark manifest 1 (because it's now in
	// the DB) and sweep manifest 2 (since it is still not in the DB)
	s.Clock.StepBy(8 * time.Hour)
	expectSuccess(t, sweepStorageJob.ProcessOne(s.Ctx))
	expectError(t, sql.ErrNoRows.Error(), sweepStorageJob.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET next_storage_sweep_at = %[1]d WHERE name = 'test1';
			DELETE FROM unknown_manifests WHERE account_name = 'test1' AND repo_name = 'foo' AND digest = '%[2]s';
			DELETE FROM unknown_manifests WHERE account_name = 'test1' AND repo_name = 'foo' AND digest = '%[3]s';
		`,
		s.Clock.Now().Add(6*time.Hour).Unix(), // next_storage_sweep_at
		testImageList1.Manifest.Digest.String(),
		testImageList2.Manifest.Digest.String(),
	)
	s.ExpectBlobsExistInStorage(t, healthyBlobs...)
	s.ExpectManifestsExistInStorage(t, "foo", healthyManifests...)
	s.ExpectManifestsExistInStorage(t, "foo",
		models.Manifest{RepositoryID: 1, Digest: testImageList1.Manifest.Digest},
	)
	s.ExpectManifestsMissingInStorage(t,
		models.Manifest{RepositoryID: 1, Digest: testImageList2.Manifest.Digest},
	)
	for manifest, reports := range healthyTrivyReports {
		for _, report := range reports {
			s.ExpectTrivyReportExistsInStorage(t, manifest, report.Format, assert.ByteData(report.Contents))
		}
	}
}

func TestSweepStorageTrivyReports(t *testing.T) {
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)
	sweepStorageJob := j.StorageSweepJob(s.Registry)
	tr, images, healthyBlobs, healthyManifests, healthyTrivyReports := setupStorageSweepTest(t, s, sweepStorageJob)

	// put some Trivy reports in the storage without adding them in the DB
	// (since the plain images have Trivy reports already, we are using the image lists for that)
	listManifest1 := healthyManifests[2]
	listManifest2 := test.GenerateImageList(images[1], images[0]).MustUpload(t, s, fooRepoRef, "")
	tr.DBChanges().Ignore()
	dummyReport1 := mustUploadDummyTrivyReport(t, s, listManifest1)
	dummyReport2 := mustUploadDummyTrivyReport(t, s, listManifest2)

	// next StorageSweepJob should mark them for deletion...
	s.Clock.StepBy(8 * time.Hour)
	expectSuccess(t, sweepStorageJob.ProcessOne(s.Ctx))
	expectError(t, sql.ErrNoRows.Error(), sweepStorageJob.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET next_storage_sweep_at = %[1]d WHERE name = 'test1';
			INSERT INTO unknown_trivy_reports (account_name, repo_name, digest, format, can_be_deleted_at) VALUES ('test1', 'foo', '%[3]s', 'json', %[2]d);
			INSERT INTO unknown_trivy_reports (account_name, repo_name, digest, format, can_be_deleted_at) VALUES ('test1', 'foo', '%[4]s', 'json', %[2]d);
		`,
		s.Clock.Now().Add(6*time.Hour).Unix(), // next_storage_sweep_at
		s.Clock.Now().Add(4*time.Hour).Unix(), // can_be_deleted_at
		listManifest2.Digest.String(), listManifest1.Digest.String())

	// ...but not delete anything yet
	s.ExpectBlobsExistInStorage(t, healthyBlobs...)
	s.ExpectManifestsExistInStorage(t, "foo", healthyManifests...)
	for manifest, reports := range healthyTrivyReports {
		for _, report := range reports {
			s.ExpectTrivyReportExistsInStorage(t, manifest, report.Format, assert.ByteData(report.Contents))
		}
	}
	s.ExpectTrivyReportExistsInStorage(t, listManifest1, dummyReport1.Format, assert.ByteData(dummyReport1.Contents))
	s.ExpectTrivyReportExistsInStorage(t, listManifest2, dummyReport2.Format, assert.ByteData(dummyReport2.Contents))

	// create a DB entry for the first Trivy report (to sort of simulate a Trivy report
	// upload that happened during StorageSweepJob was running: report was written
	// to storage already, but not yet to DB)
	mustExec(t, s.DB,
		"UPDATE trivy_security_info SET vuln_status = $1, has_enriched_report = TRUE WHERE digest = $2",
		models.CleanSeverity, listManifest1.Digest.String(),
	)
	tr.DBChanges().Ignore()

	// next StorageSweepJob should unmark report 1 (because it's now in
	// the DB) and sweep report 2 (since it is still not in the DB)
	s.Clock.StepBy(8 * time.Hour)
	expectSuccess(t, sweepStorageJob.ProcessOne(s.Ctx))
	expectError(t, sql.ErrNoRows.Error(), sweepStorageJob.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET next_storage_sweep_at = %[1]d WHERE name = 'test1';
			DELETE FROM unknown_trivy_reports WHERE account_name = 'test1' AND repo_name = 'foo' AND digest = '%[2]s' AND format = 'json';
			DELETE FROM unknown_trivy_reports WHERE account_name = 'test1' AND repo_name = 'foo' AND digest = '%[3]s' AND format = 'json';
		`,
		s.Clock.Now().Add(6*time.Hour).Unix(),
		listManifest2.Digest.String(), listManifest1.Digest.String(),
	)
	s.ExpectBlobsExistInStorage(t, healthyBlobs...)
	s.ExpectManifestsExistInStorage(t, "foo", healthyManifests...)
	for manifest, reports := range healthyTrivyReports {
		for _, report := range reports {
			s.ExpectTrivyReportExistsInStorage(t, manifest, report.Format, assert.ByteData(report.Contents))
		}
	}
	s.ExpectTrivyReportExistsInStorage(t, listManifest1, dummyReport1.Format, assert.ByteData(dummyReport1.Contents))
	s.ExpectTrivyReportMissingInStorage(t, listManifest2, dummyReport2.Format)
}
