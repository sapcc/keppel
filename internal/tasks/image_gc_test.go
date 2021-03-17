/******************************************************************************
*
*  Copyright 2021 SAP SE
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

func TestGCUntaggedImages(t *testing.T) {
	j, _, db, _, sd, clock, _ := setup(t)
	clock.StepBy(1 * time.Hour)

	//setup GC policy for test
	mustExec(t, db,
		`UPDATE accounts SET gc_policies_json = $1`,
		`[{"match_repository":".*","strategy":"delete_untagged"}]`,
	)

	//store two images, one tagged, one untagged
	images := make([]test.Image, 2)
	for idx := range images {
		image := test.GenerateImage(test.GenerateExampleLayer(int64(idx)))
		images[idx] = image

		configBlob := uploadBlob(t, db, sd, clock, image.Config)
		layerBlob := uploadBlob(t, db, sd, clock, image.Layers[0])
		uploadManifest(t, db, sd, clock, image.Manifest, image.SizeBytes())
		mustExec(t, db,
			`INSERT INTO manifest_blob_refs (blob_id, repo_id, digest) VALUES ($1, 1, $2)`,
			configBlob.ID, image.Manifest.Digest.String(),
		)
		mustExec(t, db,
			`INSERT INTO manifest_blob_refs (blob_id, repo_id, digest) VALUES ($1, 1, $2)`,
			layerBlob.ID, image.Manifest.Digest.String(),
		)
	}
	mustExec(t, db,
		`INSERT INTO tags (repo_id, name, digest, pushed_at) VALUES (1, $1, $2, $3)`,
		"first", images[0].Manifest.Digest.String(), j.timeNow(),
	)

	//GC should not do anything right now because newly-pushed images are
	//protected (to avoid deleting images that a client is about to tag)
	expectSuccess(t, j.GarbageCollectManifestsInNextRepo())
	expectError(t, sql.ErrNoRows.Error(), j.GarbageCollectManifestsInNextRepo())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/gc-untagged-images-0.sql")

	//setup GC policy that does not match
	clock.StepBy(2 * time.Hour)
	mustExec(t, db,
		`UPDATE accounts SET gc_policies_json = $1`,
		`[{"match_repository":".*","except_repository":"foo","strategy":"delete_untagged"}]`,
	)

	//GC should only update the next_gc_at timestamp, and otherwise not do anything
	expectSuccess(t, j.GarbageCollectManifestsInNextRepo())
	expectError(t, sql.ErrNoRows.Error(), j.GarbageCollectManifestsInNextRepo())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/gc-untagged-images-1.sql")

	//setup GC policy that matches
	clock.StepBy(2 * time.Hour)
	mustExec(t, db,
		`UPDATE accounts SET gc_policies_json = $1`,
		`[{"match_repository":".*","strategy":"delete_untagged"}]`,
	)
	//however now there's also a tagged image list referencing it
	imageList := test.GenerateImageList(images[0].Manifest, images[1].Manifest)
	uploadManifest(t, db, sd, clock, imageList.Manifest, imageList.SizeBytes())
	for _, image := range images {
		mustExec(t, db,
			`INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, $1, $2)`,
			imageList.Manifest.Digest.String(), image.Manifest.Digest.String(),
		)
	}
	mustExec(t, db,
		`INSERT INTO tags (repo_id, name, digest, pushed_at) VALUES (1, $1, $2, $3)`,
		"list", imageList.Manifest.Digest.String(), j.timeNow(),
	)

	//GC should not delete the untagged image since it's referenced by the tagged list image
	expectSuccess(t, j.GarbageCollectManifestsInNextRepo())
	expectError(t, sql.ErrNoRows.Error(), j.GarbageCollectManifestsInNextRepo())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/gc-untagged-images-2.sql")

	//delete the image list manifest
	clock.StepBy(2 * time.Hour)
	mustExec(t, db,
		`DELETE FROM manifests WHERE digest = $1`,
		imageList.Manifest.Digest.String(),
	)

	//GC should now delete the untagged image since nothing references it anymore
	expectSuccess(t, j.GarbageCollectManifestsInNextRepo())
	expectError(t, sql.ErrNoRows.Error(), j.GarbageCollectManifestsInNextRepo())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/gc-untagged-images-3.sql")
}
