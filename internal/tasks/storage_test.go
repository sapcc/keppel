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
	"github.com/sapcc/keppel/internal/clair"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func setupStorageSweepTest(t *testing.T, j *Janitor, db *keppel.DB, sd keppel.StorageDriver, clock *test.Clock) (images []test.Image, healthyBlobs []keppel.Blob, healthyManifests []keppel.Manifest) {
	//setup some manifests and blobs as a baseline that should never be touched by
	//SweepStorageInNextAccount
	images = make([]test.Image, 2)
	for idx := range images {
		image := test.GenerateImage(
			test.GenerateExampleLayer(int64(10*idx+1)),
			test.GenerateExampleLayer(int64(10*idx+2)),
		)
		images[idx] = image

		layer1Blob := uploadBlob(t, db, sd, clock, image.Layers[0])
		layer2Blob := uploadBlob(t, db, sd, clock, image.Layers[1])
		configBlob := uploadBlob(t, db, sd, clock, image.Config)
		healthyBlobs = append(healthyBlobs, configBlob, layer1Blob, layer2Blob)
		healthyManifests = append(healthyManifests,
			uploadManifest(t, db, sd, clock, image.Manifest, image.SizeBytes()))
		for _, blobID := range []int64{layer1Blob.ID, layer2Blob.ID, configBlob.ID} {
			mustExec(t, db,
				`INSERT INTO manifest_blob_refs (blob_id, repo_id, digest) VALUES ($1, 1, $2)`,
				blobID, image.Manifest.Digest.String(),
			)
		}
	}

	imageList := test.GenerateImageList(images[0].Manifest, images[1].Manifest)
	healthyManifests = append(healthyManifests,
		uploadManifest(t, db, sd, clock, imageList.Manifest, imageList.SizeBytes()))
	for _, image := range images {
		mustExec(t, db,
			`INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, $1, $2)`,
			imageList.Manifest.Digest.String(), image.Manifest.Digest.String(),
		)
	}

	//SweepStorageInNextAccount should run through, but not do anything besides
	//setting the storage_sweeped_at timestamp on the account
	expectSuccess(t, j.SweepStorageInNextAccount())
	expectError(t, sql.ErrNoRows.Error(), j.SweepStorageInNextAccount())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/storage-sweep-000.sql")
	expectBlobsExistInStorage(t, sd, healthyBlobs...)
	expectManifestsExistInStorage(t, sd, healthyManifests...)

	return images, healthyBlobs, healthyManifests
}

func TestSweepStorageBlobs(t *testing.T) {
	j, _, db, _, sd, clock, _ := setup(t)
	clock.StepBy(1 * time.Hour)
	_, healthyBlobs, healthyManifests := setupStorageSweepTest(t, j, db, sd, clock)

	//put some blobs in the storage without noting them in the DB
	account := keppel.Account{Name: "test1"}
	testBlob1 := test.GenerateExampleLayer(30)
	testBlob2 := test.GenerateExampleLayer(31)
	for _, blob := range []test.Bytes{testBlob1, testBlob2} {
		storageID := blob.Digest.Encoded()
		sizeBytes := uint64(len(blob.Contents))
		must(t, sd.AppendToBlob(account, storageID, 1, &sizeBytes, bytes.NewReader(blob.Contents)))
		must(t, sd.FinalizeBlob(account, storageID, 1))
	}

	//create a blob that's mid-upload; this one should be protected from sweeping
	//by the presence of the Upload object in the DB
	testBlob3 := test.GenerateExampleLayer(32)
	storageID := testBlob3.Digest.Encoded()
	sizeBytes := uint64(len(testBlob3.Contents))
	must(t, sd.AppendToBlob(account, storageID, 1, &sizeBytes, bytes.NewReader(testBlob3.Contents)))
	//^ but no FinalizeBlob() since we're still uploading!
	must(t, db.Insert(&keppel.Upload{
		RepositoryID: 1,
		UUID:         "a29d525c-2273-44ba-83a8-eafd447f1cb8", //chosen at random, but fixed
		StorageID:    storageID,
		SizeBytes:    sizeBytes,
		Digest:       testBlob3.Digest.String(),
		NumChunks:    1,
		UpdatedAt:    j.timeNow(),
	}))

	//create another blob that's mid-upload; this one will be sweeped later to
	//verify that we clean up unfinished uploads correctly
	testBlob4 := test.GenerateExampleLayer(33)
	storageID = testBlob4.Digest.Encoded()
	sizeBytes = uint64(len(testBlob4.Contents))
	must(t, sd.AppendToBlob(account, storageID, 1, &sizeBytes, bytes.NewReader(testBlob4.Contents)))

	//next SweepStorageInNextAccount should mark them for deletion...
	clock.StepBy(8 * time.Hour)
	expectSuccess(t, j.SweepStorageInNextAccount())
	expectError(t, sql.ErrNoRows.Error(), j.SweepStorageInNextAccount())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/storage-sweep-blobs-001.sql")
	//...but not delete anything yet
	expectBlobsExistInStorage(t, sd, healthyBlobs...)
	expectBlobsExistInStorage(t, sd,
		keppel.Blob{Digest: testBlob1.Digest.String(), StorageID: testBlob1.Digest.Encoded()},
		keppel.Blob{Digest: testBlob2.Digest.String(), StorageID: testBlob2.Digest.Encoded()},
		keppel.Blob{Digest: testBlob3.Digest.String(), StorageID: testBlob3.Digest.Encoded()},
		keppel.Blob{Digest: testBlob4.Digest.String(), StorageID: testBlob4.Digest.Encoded()},
	)
	expectManifestsExistInStorage(t, sd, healthyManifests...)

	//create a DB entry for the first blob (to sort of simulate an upload that
	//just got finished while SweepStorageInNextAccount was running: blob was
	//written to storage already, but not yet to DB)
	clock.StepBy(1 * time.Hour)
	dbTestBlob1 := keppel.Blob{
		AccountName: "test1",
		Digest:      testBlob1.Digest.String(),
		SizeBytes:   uint64(len(testBlob1.Contents)),
		StorageID:   testBlob1.Digest.Encoded(),
		PushedAt:    clock.Now(),
		ValidatedAt: clock.Now(),
	}
	must(t, db.Insert(&dbTestBlob1))

	//next SweepStorageInNextAccount should unmark blob 1 (because it's now in
	//the DB) and sweep blobs 2 and 4 (since it is still not in the DB)
	clock.StepBy(8 * time.Hour)
	expectSuccess(t, j.SweepStorageInNextAccount())
	expectError(t, sql.ErrNoRows.Error(), j.SweepStorageInNextAccount())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/storage-sweep-blobs-002.sql")
	expectBlobsExistInStorage(t, sd, healthyBlobs...)
	expectBlobsExistInStorage(t, sd,
		keppel.Blob{Digest: testBlob1.Digest.String(), StorageID: testBlob1.Digest.Encoded()},
		keppel.Blob{Digest: testBlob3.Digest.String(), StorageID: testBlob3.Digest.Encoded()},
	)
	expectBlobsMissingInStorage(t, sd,
		keppel.Blob{Digest: testBlob2.Digest.String(), StorageID: testBlob2.Digest.Encoded()},
		keppel.Blob{Digest: testBlob4.Digest.String(), StorageID: testBlob4.Digest.Encoded()},
	)
	expectManifestsExistInStorage(t, sd, healthyManifests...)
}

func TestSweepStorageManifests(t *testing.T) {
	j, _, db, _, sd, clock, _ := setup(t)
	clock.StepBy(1 * time.Hour)
	images, healthyBlobs, healthyManifests := setupStorageSweepTest(t, j, db, sd, clock)

	//put some manifests in the storage without nothing them in the DB
	account := keppel.Account{Name: "test1"}
	testImageList1 := test.GenerateImageList(images[0].Manifest)
	testImageList2 := test.GenerateImageList(images[1].Manifest)
	for _, manifest := range []test.Bytes{testImageList1.Manifest, testImageList2.Manifest} {
		must(t, sd.WriteManifest(account, "foo", manifest.Digest.String(), manifest.Contents))
	}

	//next SweepStorageInNextAccount should mark them for deletion...
	clock.StepBy(8 * time.Hour)
	expectSuccess(t, j.SweepStorageInNextAccount())
	expectError(t, sql.ErrNoRows.Error(), j.SweepStorageInNextAccount())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/storage-sweep-manifests-001.sql")
	//...but not delete anything yet
	expectBlobsExistInStorage(t, sd, healthyBlobs...)
	expectManifestsExistInStorage(t, sd, healthyManifests...)
	expectManifestsExistInStorage(t, sd,
		keppel.Manifest{RepositoryID: 1, Digest: testImageList1.Manifest.Digest.String()},
		keppel.Manifest{RepositoryID: 1, Digest: testImageList2.Manifest.Digest.String()},
	)

	//create a DB entry for the first manifest (to sort of simulate a manifest
	//upload that happened while SweepStorageInNextAccount: manifest was written
	//to storage already, but not yet to DB)
	clock.StepBy(1 * time.Hour)
	dbTestManifest1 := keppel.Manifest{
		RepositoryID:        1,
		Digest:              testImageList1.Manifest.Digest.String(),
		MediaType:           testImageList1.Manifest.MediaType,
		SizeBytes:           uint64(len(testImageList1.Manifest.Contents)),
		PushedAt:            clock.Now(),
		ValidatedAt:         clock.Now(),
		VulnerabilityStatus: clair.PendingVulnerabilityStatus,
	}
	must(t, db.Insert(&dbTestManifest1))

	//next SweepStorageInNextAccount should unmark manifest 1 (because it's now in
	//the DB) and sweep manifest 2 (since it is still not in the DB)
	clock.StepBy(8 * time.Hour)
	expectSuccess(t, j.SweepStorageInNextAccount())
	expectError(t, sql.ErrNoRows.Error(), j.SweepStorageInNextAccount())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/storage-sweep-manifests-002.sql")
	expectBlobsExistInStorage(t, sd, healthyBlobs...)
	expectManifestsExistInStorage(t, sd, healthyManifests...)
	expectManifestsExistInStorage(t, sd,
		keppel.Manifest{RepositoryID: 1, Digest: testImageList1.Manifest.Digest.String()},
	)
	expectManifestsMissingInStorage(t, sd,
		keppel.Manifest{RepositoryID: 1, Digest: testImageList2.Manifest.Digest.String()},
	)
}
