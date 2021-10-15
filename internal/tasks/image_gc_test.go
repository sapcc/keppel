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
	matchingGCPoliciesJSON := `[{"match_repository":".*","only_untagged":true,"action":"delete"}]`
	mustExec(t, s.DB,
		`UPDATE accounts SET gc_policies_json = $1`,
		matchingGCPoliciesJSON,
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
	tr, _ := easypg.NewTracker(t, s.DB.DbMap.Db)

	//setup GC policy that does not match
	s.Clock.StepBy(2 * time.Hour)
	ineffectiveGCPoliciesJSON := `[{"match_repository":".*","except_repository":"foo","only_untagged":true,"action":"delete"}]`
	mustExec(t, s.DB,
		`UPDATE accounts SET gc_policies_json = $1`,
		ineffectiveGCPoliciesJSON,
	)

	//GC should only update the next_gc_at timestamp and the gc_status_json field
	//(indicating that no policies match), and otherwise not do anything
	expectSuccess(t, j.GarbageCollectManifestsInNextRepo())
	expectError(t, sql.ErrNoRows.Error(), j.GarbageCollectManifestsInNextRepo())
	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET gc_policies_json = '%[1]s' WHERE name = 'test1';
			UPDATE manifests SET gc_status_json = '{"relevant_policies":[]}' WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE manifests SET gc_status_json = '{"relevant_policies":[]}' WHERE repo_id = 1 AND digest = '%[3]s';
			UPDATE repos SET next_gc_at = %[4]d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
		`,
		ineffectiveGCPoliciesJSON,
		images[0].Manifest.Digest.String(),
		images[1].Manifest.Digest.String(),
		s.Clock.Now().Add(1*time.Hour).Unix(),
	)

	//setup GC policy that matches
	s.Clock.StepBy(2 * time.Hour)
	mustExec(t, s.DB,
		`UPDATE accounts SET gc_policies_json = $1`,
		matchingGCPoliciesJSON,
	)
	//however now there's also a tagged image list referencing it
	imageList := test.GenerateImageList(images[0], images[1])
	imageList.MustUpload(t, s, fooRepoRef, "list")
	tr.DBChanges().Ignore()

	//GC should not delete the untagged image since it's referenced by the tagged list image
	expectSuccess(t, j.GarbageCollectManifestsInNextRepo())
	expectError(t, sql.ErrNoRows.Error(), j.GarbageCollectManifestsInNextRepo())
	tr.DBChanges().AssertEqualf(`
			UPDATE manifests SET gc_status_json = '{"protected_by_parent":"%[1]s"}' WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE manifests SET gc_status_json = '{"protected_by_parent":"%[1]s"}' WHERE repo_id = 1 AND digest = '%[3]s';
			UPDATE manifests SET gc_status_json = '{"protected_by_recent_upload":true}' WHERE repo_id = 1 AND digest = '%[1]s';
			UPDATE repos SET next_gc_at = %[4]d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
		`,
		imageList.Manifest.Digest.String(),
		images[0].Manifest.Digest.String(),
		images[1].Manifest.Digest.String(),
		s.Clock.Now().Add(1*time.Hour).Unix(),
	)

	//delete the image list manifest
	s.Clock.StepBy(2 * time.Hour)
	mustExec(t, s.DB,
		`DELETE FROM manifests WHERE digest = $1`,
		imageList.Manifest.Digest.String(),
	)
	tr.DBChanges().Ignore()

	//GC should now delete the untagged image since nothing references it anymore
	expectSuccess(t, j.GarbageCollectManifestsInNextRepo())
	expectError(t, sql.ErrNoRows.Error(), j.GarbageCollectManifestsInNextRepo())
	tr.DBChanges().AssertEqualf(`
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[2]s' AND blob_id = 3;
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[2]s' AND blob_id = 4;
			DELETE FROM manifest_contents WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE manifests SET gc_status_json = '{"relevant_policies":%[3]s}' WHERE repo_id = 1 AND digest = '%[1]s';
			DELETE FROM manifests WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE repos SET next_gc_at = %[4]d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
		`,
		images[0].Manifest.Digest.String(),
		images[1].Manifest.Digest.String(),
		matchingGCPoliciesJSON,
		s.Clock.Now().Add(1*time.Hour).Unix(),
	)
}
