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
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)

	//setup GC policy for test
	mustExec(t, s.DB,
		`UPDATE accounts SET gc_policies_json = $1`,
		`[{"match_repository":".*","strategy":"delete_untagged"}]`,
	)

	//store two images, one tagged, one untagged
	images := []test.Image{
		test.GenerateImage(test.GenerateExampleLayer(0)),
		test.GenerateImage(test.GenerateExampleLayer(1)),
	}
	images[0].MustUpload(t, s, fooRepoRef, "first")
	images[1].MustUpload(t, s, fooRepoRef, "")

	//GC should not do anything right now because newly-pushed images are
	//protected (to avoid deleting images that a client is about to tag)
	expectSuccess(t, j.GarbageCollectManifestsInNextRepo())
	expectError(t, sql.ErrNoRows.Error(), j.GarbageCollectManifestsInNextRepo())
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/gc-untagged-images-0.sql")

	//setup GC policy that does not match
	s.Clock.StepBy(2 * time.Hour)
	mustExec(t, s.DB,
		`UPDATE accounts SET gc_policies_json = $1`,
		`[{"match_repository":".*","except_repository":"foo","strategy":"delete_untagged"}]`,
	)

	//GC should only update the next_gc_at timestamp, and otherwise not do anything
	expectSuccess(t, j.GarbageCollectManifestsInNextRepo())
	expectError(t, sql.ErrNoRows.Error(), j.GarbageCollectManifestsInNextRepo())
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/gc-untagged-images-1.sql")

	//setup GC policy that matches
	s.Clock.StepBy(2 * time.Hour)
	mustExec(t, s.DB,
		`UPDATE accounts SET gc_policies_json = $1`,
		`[{"match_repository":".*","strategy":"delete_untagged"}]`,
	)
	//however now there's also a tagged image list referencing it
	imageList := test.GenerateImageList(images[0], images[1])
	imageList.MustUpload(t, s, fooRepoRef, "list")

	//GC should not delete the untagged image since it's referenced by the tagged list image
	expectSuccess(t, j.GarbageCollectManifestsInNextRepo())
	expectError(t, sql.ErrNoRows.Error(), j.GarbageCollectManifestsInNextRepo())
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/gc-untagged-images-2.sql")

	//delete the image list manifest
	s.Clock.StepBy(2 * time.Hour)
	mustExec(t, s.DB,
		`DELETE FROM manifests WHERE digest = $1`,
		imageList.Manifest.Digest.String(),
	)

	//GC should now delete the untagged image since nothing references it anymore
	expectSuccess(t, j.GarbageCollectManifestsInNextRepo())
	expectError(t, sql.ErrNoRows.Error(), j.GarbageCollectManifestsInNextRepo())
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/gc-untagged-images-3.sql")
}
