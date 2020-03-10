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
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

//Base behavior for various unit tests that start with the same image, destroy
//it in various ways, and check that ValidateNextManifest correctly fixes it.
func testValidateNextManifestFixesDisturbance(t *testing.T, disturb func(*keppel.DB, []int64, []string)) {
	j, _, db, sd, clock := setup(t)
	clock.StepBy(1 * time.Hour)

	var (
		allBlobIDs         []int64
		allManifestDigests []string
	)

	//setup two image manifests, both with some layers
	images := make([]test.Image, 2)
	for idx := range images {
		image := test.GenerateImage(
			test.GenerateExampleLayer(int64(10*idx+1)),
			test.GenerateExampleLayer(int64(10*idx+2)),
		)
		images[idx] = image

		layer1BlobID := uploadBlob(t, db, sd, clock, image.Layers[0])
		layer2BlobID := uploadBlob(t, db, sd, clock, image.Layers[1])
		configBlobID := uploadBlob(t, db, sd, clock, image.Config)
		uploadManifest(t, db, sd, clock, image.Manifest, image.SizeBytes())
		for _, blobID := range []int64{layer1BlobID, layer2BlobID, configBlobID} {
			mustExec(t, db,
				`INSERT INTO manifest_blob_refs (blob_id, repo_id, digest) VALUES ($1, 1, $2)`,
				blobID, image.Manifest.Digest.String(),
			)
		}
		allBlobIDs = append(allBlobIDs, layer1BlobID, layer2BlobID, configBlobID)
		allManifestDigests = append(allManifestDigests, image.Manifest.Digest.String())
	}

	//also setup an image list manifest containing those images (so that we have
	//some manifest-manifest refs to play with)
	imageList := test.GenerateImageList(images[0].Manifest, images[1].Manifest)
	uploadManifest(t, db, sd, clock, imageList.Manifest, imageList.SizeBytes())
	for _, image := range images {
		mustExec(t, db,
			`INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, $1, $2)`,
			imageList.Manifest.Digest.String(), image.Manifest.Digest.String(),
		)
	}
	allManifestDigests = append(allManifestDigests, imageList.Manifest.Digest.String())

	//since these manifests were just uploaded, validated_at is set to right now,
	//so ValidateNextManifest will report that there is nothing to do
	expectError(t, sql.ErrNoRows.Error(), j.ValidateNextManifest())

	//once they need validating, they validate successfully
	clock.StepBy(12 * time.Hour)
	expectSuccess(t, j.ValidateNextManifest())
	expectSuccess(t, j.ValidateNextManifest())
	expectSuccess(t, j.ValidateNextManifest())
	expectError(t, sql.ErrNoRows.Error(), j.ValidateNextManifest())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/manifest-validate-001-before-disturbance.sql")

	//disturb the DB state, then rerun ValidateNextManifest to fix it
	clock.StepBy(12 * time.Hour)
	disturb(db, allBlobIDs, allManifestDigests)
	expectSuccess(t, j.ValidateNextManifest())
	expectSuccess(t, j.ValidateNextManifest())
	expectSuccess(t, j.ValidateNextManifest())
	expectError(t, sql.ErrNoRows.Error(), j.ValidateNextManifest())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/manifest-validate-002-after-fix.sql")
}

func TestValidateNextManifestFixesWrongSize(t *testing.T) {
	testValidateNextManifestFixesDisturbance(t, func(db *keppel.DB, allBlobIDs []int64, allManifestDigests []string) {
		mustExec(t, db, `UPDATE manifests SET size_bytes = 1337`)
	})
}

func TestValidateNextManifestFixesMissingManifestBlobRefs(t *testing.T) {
	testValidateNextManifestFixesDisturbance(t, func(db *keppel.DB, allBlobIDs []int64, allManifestDigests []string) {
		mustExec(t, db, `DELETE FROM manifest_blob_refs WHERE blob_id % 2 = 0`)
	})
}

func TestValidateNextManifestFixesMissingManifestManifestRefs(t *testing.T) {
	testValidateNextManifestFixesDisturbance(t, func(db *keppel.DB, allBlobIDs []int64, allManifestDigests []string) {
		mustExec(t, db, `DELETE FROM manifest_manifest_refs`)
	})
}

func TestValidateNextManifestFixesSuperfluousManifestBlobRefs(t *testing.T) {
	testValidateNextManifestFixesDisturbance(t, func(db *keppel.DB, allBlobIDs []int64, allManifestDigests []string) {
		for _, id := range allBlobIDs {
			for _, d := range allManifestDigests {
				mustExec(t, db, `INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, $1, $2) ON CONFLICT DO NOTHING`, d, id)
			}
		}
	})
}

func TestValidateNextManifestFixesSuperfluousManifestManifestRefs(t *testing.T) {
	testValidateNextManifestFixesDisturbance(t, func(db *keppel.DB, allBlobIDs []int64, allManifestDigests []string) {
		for _, d1 := range allManifestDigests {
			for _, d2 := range allManifestDigests {
				mustExec(t, db, `INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, $1, $2) ON CONFLICT DO NOTHING`, d1, d2)
			}
		}
	})
}

func TestValidateNextManifestError(t *testing.T) {
	j, _, db, sd, clock := setup(t)

	//setup a manifest that does not exist in the backing storage
	clock.StepBy(1 * time.Hour)
	image := test.GenerateImage( /* no layers */ )
	must(t, db.Insert(&keppel.Manifest{
		RepositoryID: 1,
		Digest:       image.Manifest.Digest.String(),
		MediaType:    image.Manifest.MediaType,
		SizeBytes:    image.SizeBytes(),
		PushedAt:     clock.Now(),
		ValidatedAt:  clock.Now(),
	}))

	//validation should yield an error ("no such manifest" is returned by test.StorageDriver)
	clock.StepBy(12 * time.Hour)
	expectError(t, "while validating a manifest: no such manifest",
		j.ValidateNextManifest())

	//check that validation error to be recorded in the DB
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/manifest-validate-error-001.sql")

	//expect next ValidateNextManifest run to skip over this manifest since it
	//was recently validated
	expectError(t, sql.ErrNoRows.Error(), j.ValidateNextManifest())

	//upload manifest and blob so that we can test recovering from the validation error
	uploadBlob(t, db, sd, clock, image.Config)
	mustExec(t, db, `DELETE FROM manifests WHERE digest = $1`, image.Manifest.Digest.String())
	uploadManifest(t, db, sd, clock, image.Manifest, image.SizeBytes())

	//next validation should be happy (and also create the missing refs)
	clock.StepBy(12 * time.Hour)
	expectSuccess(t, j.ValidateNextManifest())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/manifest-validate-error-002.sql")
}
