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
	"database/sql"
	"testing"
	"time"

	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/keppel/internal/test"
)

func TestSweepBlobMounts(t *testing.T) {
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)

	//setup an image manifest with some layers, so that we have some blob mounts
	//that shall not be sweeped
	image := test.GenerateImage(
		test.GenerateExampleLayer(1),
		test.GenerateExampleLayer(2),
	)
	layer1Blob := uploadBlob(t, s, image.Layers[0])
	layer2Blob := uploadBlob(t, s, image.Layers[1])
	configBlob := uploadBlob(t, s, image.Config)
	uploadManifest(t, s, image.Manifest, image.SizeBytes())
	for _, blobID := range []int64{layer1Blob.ID, layer2Blob.ID, configBlob.ID} {
		mustExec(t, s.DB,
			`INSERT INTO manifest_blob_refs (blob_id, repo_id, digest) VALUES ($1, 1, $2)`,
			blobID, image.Manifest.Digest.String(),
		)
	}

	//the blob mount sweep should not mark any blob mount for deletion since they
	//are all in use, but should set the blob_mounts_sweeped_at timestamp on the
	//repo
	expectSuccess(t, j.SweepBlobMountsInNextRepo())
	expectError(t, sql.ErrNoRows.Error(), j.SweepBlobMountsInNextRepo())
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/blob-mount-sweep-001.sql")

	//upload two blobs that are not referenced by any manifest
	s.Clock.StepBy(2 * time.Hour)
	bogusBlob1 := test.GenerateExampleLayer(3)
	bogusBlob2 := test.GenerateExampleLayer(4)
	dbBogusBlob1 := uploadBlob(t, s, bogusBlob1)
	dbBogusBlob2 := uploadBlob(t, s, bogusBlob2)
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/blob-mount-sweep-002.sql")

	//the next sweep should mark those blob's mounts for deletion
	expectSuccess(t, j.SweepBlobMountsInNextRepo())
	expectError(t, sql.ErrNoRows.Error(), j.SweepBlobMountsInNextRepo())
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/blob-mount-sweep-003.sql")

	//save one of those blob mounts from deletion by creating a manifest-blob
	//reference for it (this reference is actually bogus and would be removed by
	//ValidateNextManifest, but we're not testing that here)
	mustExec(t, s.DB,
		`INSERT INTO manifest_blob_refs (blob_id, repo_id, digest) VALUES ($1, 1, $2)`,
		dbBogusBlob2.ID, image.Manifest.Digest.String(),
	)
	_ = dbBogusBlob1

	//the next sweep will delete the mount for `bogusBlob1` (since it was marked
	//for deletion and is still not referenced by any manifest), but remove the
	//mark on the mount for `bogusBlob2` (since it is now referenced by a
	//manifest)
	s.Clock.StepBy(2 * time.Hour)
	expectSuccess(t, j.SweepBlobMountsInNextRepo())
	expectError(t, sql.ErrNoRows.Error(), j.SweepBlobMountsInNextRepo())
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/blob-mount-sweep-004.sql")
}
