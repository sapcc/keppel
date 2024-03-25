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

package tasks

import (
	"bytes"
	"database/sql"
	"testing"
	"time"

	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/jobloop"

	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func setupStorageSweepTest(t *testing.T, s test.Setup, sweepStorageJob jobloop.Job) (images []test.Image, healthyBlobs []models.Blob, healthyManifests []models.Manifest) {
	// setup some manifests and blobs as a baseline that should never be touched by
	// StorageSweepJob
	images = make([]test.Image, 2)
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
		healthyManifests = append(healthyManifests,
			image.MustUpload(t, s, fooRepoRef, ""),
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
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/storage-sweep-000.sql")
	s.ExpectBlobsExistInStorage(t, healthyBlobs...)
	s.ExpectManifestsExistInStorage(t, "foo", healthyManifests...)

	return images, healthyBlobs, healthyManifests
}

func TestSweepStorageBlobs(t *testing.T) {
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)
	sweepStorageJob := j.StorageSweepJob(s.Registry)
	_, healthyBlobs, healthyManifests := setupStorageSweepTest(t, s, sweepStorageJob)

	// put some blobs in the storage without adding them in the DB
	account := models.Account{Name: "test1"}
	testBlob1 := test.GenerateExampleLayer(30)
	testBlob2 := test.GenerateExampleLayer(31)
	for _, blob := range []test.Bytes{testBlob1, testBlob2} {
		storageID := blob.Digest.Encoded()
		sizeBytes := uint64(len(blob.Contents))
		mustDo(t, s.SD.AppendToBlob(account, storageID, 1, &sizeBytes, bytes.NewReader(blob.Contents)))
		mustDo(t, s.SD.FinalizeBlob(account, storageID, 1))
	}

	// create a blob that's mid-upload; this one should be protected from sweeping
	// by the presence of the Upload object in the DB
	testBlob3 := test.GenerateExampleLayer(32)
	storageID := testBlob3.Digest.Encoded()
	sizeBytes := uint64(len(testBlob3.Contents))
	mustDo(t, s.SD.AppendToBlob(account, storageID, 1, &sizeBytes, bytes.NewReader(testBlob3.Contents)))
	// ^ but no FinalizeBlob() since we're still uploading!
	mustDo(t, s.DB.Insert(&models.Upload{
		RepositoryID: 1,
		UUID:         "a29d525c-2273-44ba-83a8-eafd447f1cb8", // chosen at random, but fixed
		StorageID:    storageID,
		SizeBytes:    sizeBytes,
		Digest:       testBlob3.Digest.String(),
		NumChunks:    1,
		UpdatedAt:    j.timeNow(),
	}))

	// create another blob that's mid-upload; this one will be sweeped later to
	// verify that we clean up unfinished uploads correctly
	testBlob4 := test.GenerateExampleLayer(33)
	storageID = testBlob4.Digest.Encoded()
	sizeBytes = uint64(len(testBlob4.Contents))
	mustDo(t, s.SD.AppendToBlob(account, storageID, 1, &sizeBytes, bytes.NewReader(testBlob4.Contents)))

	// next StorageSweepJob should mark them for deletion...
	s.Clock.StepBy(8 * time.Hour)
	expectSuccess(t, sweepStorageJob.ProcessOne(s.Ctx))
	expectError(t, sql.ErrNoRows.Error(), sweepStorageJob.ProcessOne(s.Ctx))
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/storage-sweep-blobs-001.sql")
	// ...but not delete anything yet
	s.ExpectBlobsExistInStorage(t, healthyBlobs...)
	s.ExpectBlobsExistInStorage(t,
		models.Blob{AccountName: "test1", Digest: testBlob1.Digest, StorageID: testBlob1.Digest.Encoded()},
		models.Blob{AccountName: "test1", Digest: testBlob2.Digest, StorageID: testBlob2.Digest.Encoded()},
		models.Blob{AccountName: "test1", Digest: testBlob3.Digest, StorageID: testBlob3.Digest.Encoded()},
		models.Blob{AccountName: "test1", Digest: testBlob4.Digest, StorageID: testBlob4.Digest.Encoded()},
	)
	s.ExpectManifestsExistInStorage(t, "foo", healthyManifests...)

	// create a DB entry for the first blob (to sort of simulate an upload that
	// just got finished while StorageSweepJob was running: blob was
	// written to storage already, but not yet to DB)
	s.Clock.StepBy(1 * time.Hour)
	dbTestBlob1 := models.Blob{
		AccountName: "test1",
		Digest:      testBlob1.Digest,
		SizeBytes:   uint64(len(testBlob1.Contents)),
		StorageID:   testBlob1.Digest.Encoded(),
		PushedAt:    s.Clock.Now(),
		ValidatedAt: s.Clock.Now(),
	}
	mustDo(t, s.DB.Insert(&dbTestBlob1))

	// next StorageSweepJob should unmark blob 1 (because it's now in
	// the DB) and sweep blobs 2 and 4 (since it is still not in the DB)
	s.Clock.StepBy(8 * time.Hour)
	expectSuccess(t, sweepStorageJob.ProcessOne(s.Ctx))
	expectError(t, sql.ErrNoRows.Error(), sweepStorageJob.ProcessOne(s.Ctx))
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/storage-sweep-blobs-002.sql")
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
}

func TestSweepStorageManifests(t *testing.T) {
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)
	sweepStorageJob := j.StorageSweepJob(s.Registry)
	images, healthyBlobs, healthyManifests := setupStorageSweepTest(t, s, sweepStorageJob)

	// put some manifests in the storage without adding them in the DB
	account := models.Account{Name: "test1"}
	testImageList1 := test.GenerateImageList(images[0])
	testImageList2 := test.GenerateImageList(images[1])
	for _, manifest := range []test.Bytes{testImageList1.Manifest, testImageList2.Manifest} {
		mustDo(t, s.SD.WriteManifest(account, "foo", manifest.Digest, manifest.Contents))
	}

	// next StorageSweepJob should mark them for deletion...
	s.Clock.StepBy(8 * time.Hour)
	expectSuccess(t, sweepStorageJob.ProcessOne(s.Ctx))
	expectError(t, sql.ErrNoRows.Error(), sweepStorageJob.ProcessOne(s.Ctx))
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/storage-sweep-manifests-001.sql")
	// ...but not delete anything yet
	s.ExpectBlobsExistInStorage(t, healthyBlobs...)
	s.ExpectManifestsExistInStorage(t, "foo", healthyManifests...)
	s.ExpectManifestsExistInStorage(t, "foo",
		models.Manifest{RepositoryID: 1, Digest: testImageList1.Manifest.Digest},
		models.Manifest{RepositoryID: 1, Digest: testImageList2.Manifest.Digest},
	)

	// create a DB entry for the first manifest (to sort of simulate a manifest
	// upload that happened while StorageSweepJob: manifest was written
	// to storage already, but not yet to DB)
	s.Clock.StepBy(1 * time.Hour)
	mustDo(t, s.DB.Insert(&models.Manifest{
		RepositoryID: 1,
		Digest:       testImageList1.Manifest.Digest,
		MediaType:    testImageList1.Manifest.MediaType,
		SizeBytes:    uint64(len(testImageList1.Manifest.Contents)),
		PushedAt:     s.Clock.Now(),
		ValidatedAt:  s.Clock.Now(),
	}))

	// next StorageSweepJob should unmark manifest 1 (because it's now in
	// the DB) and sweep manifest 2 (since it is still not in the DB)
	s.Clock.StepBy(8 * time.Hour)
	expectSuccess(t, sweepStorageJob.ProcessOne(s.Ctx))
	expectError(t, sql.ErrNoRows.Error(), sweepStorageJob.ProcessOne(s.Ctx))
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/storage-sweep-manifests-002.sql")
	s.ExpectBlobsExistInStorage(t, healthyBlobs...)
	s.ExpectManifestsExistInStorage(t, "foo", healthyManifests...)
	s.ExpectManifestsExistInStorage(t, "foo",
		models.Manifest{RepositoryID: 1, Digest: testImageList1.Manifest.Digest},
	)
	s.ExpectManifestsMissingInStorage(t,
		models.Manifest{RepositoryID: 1, Digest: testImageList2.Manifest.Digest},
	)
}
