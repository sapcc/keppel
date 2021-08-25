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
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/keppel/internal/api"
	"github.com/sapcc/keppel/internal/clair"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

////////////////////////////////////////////////////////////////////////////////
// tests for ValidateNextManifest

//Base behavior for various unit tests that start with the same image list, destroy
//it in various ways, and check that ValidateNextManifest correctly fixes it.
func testValidateNextManifestFixesDisturbance(t *testing.T, disturb func(*keppel.DB, []int64, []string)) {
	j, _, db, _, sd, clock, _ := setup(t)
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

		layer1Blob := uploadBlob(t, db, sd, clock, image.Layers[0])
		layer2Blob := uploadBlob(t, db, sd, clock, image.Layers[1])
		configBlob := uploadBlob(t, db, sd, clock, image.Config)
		uploadManifest(t, db, sd, clock, image.Manifest, image.SizeBytes())
		for _, blobID := range []int64{layer1Blob.ID, layer2Blob.ID, configBlob.ID} {
			mustExec(t, db,
				`INSERT INTO manifest_blob_refs (blob_id, repo_id, digest) VALUES ($1, 1, $2)`,
				blobID, image.Manifest.Digest.String(),
			)
		}
		allBlobIDs = append(allBlobIDs, layer1Blob.ID, layer2Blob.ID, configBlob.ID)
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
	clock.StepBy(36 * time.Hour)
	expectSuccess(t, j.ValidateNextManifest())
	expectSuccess(t, j.ValidateNextManifest())
	expectSuccess(t, j.ValidateNextManifest())
	expectError(t, sql.ErrNoRows.Error(), j.ValidateNextManifest())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/manifest-validate-001-before-disturbance.sql")

	//disturb the DB state, then rerun ValidateNextManifest to fix it
	clock.StepBy(36 * time.Hour)
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
	j, _, db, _, sd, clock, _ := setup(t)

	//setup a manifest that is missing a referenced blob
	clock.StepBy(1 * time.Hour)
	image := test.GenerateImage( /* no layers */ )
	uploadManifest(t, db, sd, clock, image.Manifest, image.SizeBytes())

	//validation should yield an error
	clock.StepBy(36 * time.Hour)
	expectedError := "while validating a manifest: manifest blob unknown to registry: " + image.Config.Digest.String()
	expectError(t, expectedError, j.ValidateNextManifest())

	//check that validation error to be recorded in the DB
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/manifest-validate-error-001.sql")

	//expect next ValidateNextManifest run to skip over this manifest since it
	//was recently validated
	expectError(t, sql.ErrNoRows.Error(), j.ValidateNextManifest())

	//upload missing blob so that we can test recovering from the validation error
	uploadBlob(t, db, sd, clock, image.Config)

	//next validation should be happy (and also create the missing refs)
	clock.StepBy(36 * time.Hour)
	expectSuccess(t, j.ValidateNextManifest())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/manifest-validate-error-002.sql")
}

////////////////////////////////////////////////////////////////////////////////
// tests for SyncManifestsInNextRepo

func TestSyncManifestsInNextRepo(t *testing.T) {
	forAllReplicaTypes(t, func(strategy string) {
		j1, _, db1, _, sd1, clock, h1 := setup(t)
		j2, _, db2, sd2, _ := setupReplica(t, db1, h1, clock, strategy)
		clock.StepBy(1 * time.Hour)

		//upload some manifests...
		images := make([]test.Image, 4)
		for idx := range images {
			image := test.GenerateImage(
				test.GenerateExampleLayer(int64(10*idx+1)),
				test.GenerateExampleLayer(int64(10*idx+2)),
			)
			images[idx] = image

			//...to the primary account...
			layer1Blob := uploadBlob(t, db1, sd1, clock, image.Layers[0])
			layer2Blob := uploadBlob(t, db1, sd1, clock, image.Layers[1])
			configBlob := uploadBlob(t, db1, sd1, clock, image.Config)
			uploadManifest(t, db1, sd1, clock, image.Manifest, image.SizeBytes())
			for _, blobID := range []int64{layer1Blob.ID, layer2Blob.ID, configBlob.ID} {
				mustExec(t, db1,
					`INSERT INTO manifest_blob_refs (blob_id, repo_id, digest) VALUES ($1, 1, $2)`,
					blobID, image.Manifest.Digest.String(),
				)
			}

			//...and most of them also to the replica account (to simulate replication having taken place)
			if idx != 0 {
				layer1Blob := uploadBlob(t, db2, sd2, clock, image.Layers[0])
				layer2Blob := uploadBlob(t, db2, sd2, clock, image.Layers[1])
				configBlob := uploadBlob(t, db2, sd2, clock, image.Config)
				uploadManifest(t, db2, sd2, clock, image.Manifest, image.SizeBytes())
				for _, blobID := range []int64{layer1Blob.ID, layer2Blob.ID, configBlob.ID} {
					mustExec(t, db2,
						`INSERT INTO manifest_blob_refs (blob_id, repo_id, digest) VALUES ($1, 1, $2)`,
						blobID, image.Manifest.Digest.String(),
					)
				}
			}
		}

		//some of the replicated images are also tagged
		for _, db := range []*keppel.DB{db1, db2} {
			for _, tagName := range []string{"latest", "other"} {
				mustExec(t, db,
					`INSERT INTO tags (repo_id, name, digest, pushed_at) VALUES (1, $1, $2, $3)`,
					tagName,
					images[1].Manifest.Digest.String(),
					clock.Now(),
				)
			}
		}

		//we need some quota for this since the tag sync runs
		//Processor.ReplicateManifest() which insists on running a quota check before
		//storing manifests, even if the new manifest ends up an existing one and
		//therefore doesn't affect the quota usage at all
		mustExec(t, db2,
			`INSERT INTO quotas (auth_tenant_id, manifests) VALUES ($1, $2)`,
			"test1authtenant", 10,
		)

		//also setup an image list manifest containing some of those images (so that we have
		//some manifest-manifest refs to play with)
		imageList := test.GenerateImageList(images[1].Manifest, images[2].Manifest)
		uploadManifest(t, db1, sd1, clock, imageList.Manifest, imageList.SizeBytes())
		for _, imageManifest := range imageList.ImageManifests {
			mustExec(t, db1,
				`INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, $1, $2)`,
				imageList.Manifest.Digest.String(), imageManifest.Digest.String(),
			)
		}
		//this one is replicated as well
		uploadManifest(t, db2, sd2, clock, imageList.Manifest, imageList.SizeBytes())
		for _, imageManifest := range imageList.ImageManifests {
			mustExec(t, db2,
				`INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, $1, $2)`,
				imageList.Manifest.Digest.String(), imageManifest.Digest.String(),
			)
		}

		//set a well-known last_pulled_at timestamp on all manifests in the primary
		//DB (we will later verify that this was not touched by the manifest sync)
		initialLastPulledAt := time.Unix(42, 0)
		mustExec(t, db1, `UPDATE manifests SET last_pulled_at = $1`, initialLastPulledAt)
		mustExec(t, db1, `UPDATE tags SET last_pulled_at = $1`, initialLastPulledAt)

		//as an exception, in the on_first_use method, we can and want to merge
		//last_pulled_at timestamps from the replica into those of the primary, so
		//set some of those to verify the merging behavior
		earlierLastPulledAt := initialLastPulledAt.Add(-10 * time.Second)
		laterLastPulledAt := initialLastPulledAt.Add(+10 * time.Second)
		mustExec(t, db2, `UPDATE manifests SET last_pulled_at = $1 WHERE digest = $2`, earlierLastPulledAt, images[1].Manifest.Digest.String())
		mustExec(t, db2, `UPDATE manifests SET last_pulled_at = $1 WHERE digest = $2`, laterLastPulledAt, images[2].Manifest.Digest.String())
		mustExec(t, db2, `UPDATE tags SET last_pulled_at = $1 WHERE name = $2`, earlierLastPulledAt, "latest")
		mustExec(t, db2, `UPDATE tags SET last_pulled_at = $1 WHERE name = $2`, laterLastPulledAt, "other")

		tr, tr0 := easypg.NewTracker(t, db2.DbMap.Db)
		tr0.AssertEqualToFile(fmt.Sprintf("fixtures/manifest-sync-setup-%s.sql", strategy))
		trForPrimary, _ := easypg.NewTracker(t, db1.DbMap.Db)

		//SyncManifestsInNextRepo on the primary registry should have nothing to do
		//since there are no replica accounts
		expectError(t, sql.ErrNoRows.Error(), j1.SyncManifestsInNextRepo())
		trForPrimary.DBChanges().AssertEmpty()
		//SyncManifestsInNextRepo on the secondary registry should set the
		//ManifestsSyncedAt timestamp on the repo, but otherwise not do anything
		expectSuccess(t, j2.SyncManifestsInNextRepo())
		tr.DBChanges().AssertEqualf(`
			UPDATE repos SET next_manifest_sync_at = %d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
		`,
			clock.Now().Add(1*time.Hour).Unix(),
		)
		//second run should not have anything else to do
		expectError(t, sql.ErrNoRows.Error(), j2.SyncManifestsInNextRepo())
		tr.DBChanges().AssertEmpty()

		//in on_first_use, the sync should have merged the replica's last_pulled_at
		//timestamps into the primary, i.e. primary.last_pulled_at =
		//max(primary.last_pulled_at, replica.last_pulled_at); this only touches
		//the DB when the replica's last_pulled_at is after the primary's
		if strategy == "on_first_use" {
			trForPrimary.DBChanges().AssertEqualf(
				`
				UPDATE manifests SET last_pulled_at = %[1]d WHERE repo_id = 1 AND digest = '%[2]s';
UPDATE tags SET last_pulled_at = %[1]d WHERE repo_id = 1 AND name = 'other';
				`,
				laterLastPulledAt.Unix(),
				images[2].Manifest.Digest.String(),
			)
			//reset all timestamps to prevent divergences in the rest of the test
			mustExec(t, db1, `UPDATE manifests SET last_pulled_at = $1`, initialLastPulledAt)
			mustExec(t, db1, `UPDATE tags SET last_pulled_at = $1`, initialLastPulledAt)
			mustExec(t, db2, `UPDATE manifests SET last_pulled_at = $1`, initialLastPulledAt)
			mustExec(t, db2, `UPDATE tags SET last_pulled_at = $1`, initialLastPulledAt)
			tr.DBChanges() // skip these changes
		} else {
			trForPrimary.DBChanges().AssertEmpty()
		}

		//delete a manifest on the primary side (this one is a simple image not referenced by anyone else)
		clock.StepBy(2 * time.Hour)
		mustExec(t, db1,
			`DELETE FROM manifests WHERE digest = $1`,
			images[3].Manifest.Digest.String(),
		)
		//move a tag on the primary side
		mustExec(t, db1,
			`UPDATE tags SET digest = $1 WHERE name = 'latest'`,
			images[2].Manifest.Digest.String(),
		)

		//again, nothing to do on the primary side
		expectError(t, sql.ErrNoRows.Error(), j1.SyncManifestsInNextRepo())
		//SyncManifestsInNextRepo on the replica side should not do anything while
		//the account is in maintenance; only the timestamp is updated to make sure
		//that the job loop progresses to the next repo
		mustExec(t, db2, `UPDATE accounts SET in_maintenance = TRUE`)
		expectSuccess(t, j2.SyncManifestsInNextRepo())
		tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET in_maintenance = TRUE WHERE name = 'test1';
			UPDATE repos SET next_manifest_sync_at = %d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
		`,
			clock.Now().Add(1*time.Hour).Unix(),
		)
		expectError(t, sql.ErrNoRows.Error(), j2.SyncManifestsInNextRepo())
		tr.DBChanges().AssertEmpty()

		//end maintenance
		mustExec(t, db2, `UPDATE accounts SET in_maintenance = FALSE`)
		tr.DBChanges().AssertEqual(`UPDATE accounts SET in_maintenance = FALSE WHERE name = 'test1';`)

		//test that replication from external uses the inbound cache
		if strategy == "from_external_on_first_use" {
			//after the end of the maintenance, we would naively expect
			//SyncManifestsInNextRepo to actually replicate the deletion, BUT we have an
			//inbound cache with a lifetime of 6 hours, so actually nothing should
			//happen (only the tag gets synced, which includes a validation of the
			//referenced manifest)
			clock.StepBy(2 * time.Hour)
			expectSuccess(t, j2.SyncManifestsInNextRepo())
			tr.DBChanges().AssertEqualf(`
			UPDATE manifests SET validated_at = %d WHERE repo_id = 1 AND digest = '%s';
			UPDATE repos SET next_manifest_sync_at = %d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
		`,
				clock.Now().Unix(),
				images[1].Manifest.Digest.String(),
				clock.Now().Add(1*time.Hour).Unix(),
			)
			expectError(t, sql.ErrNoRows.Error(), j2.SyncManifestsInNextRepo())
			tr.DBChanges().AssertEmpty()
		}

		//From now on, we will go in clock increments of 7 hours to force the
		//inbound cache to never hit.

		//after the end of the maintenance, SyncManifestsInNextRepo on the replica
		//side should delete the same manifest that we deleted in the primary
		//account, and also replicate the tag change (which includes a validation
		//of the tagged manifests)
		clock.StepBy(7 * time.Hour)
		expectSuccess(t, j2.SyncManifestsInNextRepo())
		manifestValidationBecauseOfExistingTag := fmt.Sprintf(
			//this validation is skipped in "on_first_use" because the respective tag is unchanged
			`UPDATE manifests SET validated_at = %d WHERE repo_id = 1 AND digest = '%s';`+"\n",
			clock.Now().Unix(), images[1].Manifest.Digest.String(),
		)
		if strategy == "on_first_use" {
			manifestValidationBecauseOfExistingTag = ""
		}
		tr.DBChanges().AssertEqualf(`
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[1]s' AND blob_id = 7;
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[1]s' AND blob_id = 8;
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[1]s' AND blob_id = 9;
			DELETE FROM manifest_contents WHERE repo_id = 1 AND digest = '%[1]s';
			%[5]sDELETE FROM manifests WHERE repo_id = 1 AND digest = '%[1]s';
			UPDATE manifests SET validated_at = %[2]d WHERE repo_id = 1 AND digest = '%[3]s';
			UPDATE repos SET next_manifest_sync_at = %[4]d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
			UPDATE tags SET digest = '%[3]s', pushed_at = %[2]d, last_pulled_at = NULL WHERE repo_id = 1 AND name = 'latest';
		`,
			images[3].Manifest.Digest.String(), //the deleted manifest
			clock.Now().Unix(),
			images[2].Manifest.Digest.String(), //the manifest now tagged as "latest"
			clock.Now().Add(1*time.Hour).Unix(),
			manifestValidationBecauseOfExistingTag,
		)
		expectError(t, sql.ErrNoRows.Error(), j2.SyncManifestsInNextRepo())
		tr.DBChanges().AssertEmpty()

		//cause a deliberate inconsistency on the primary side: delete a manifest that
		//*is* referenced by another manifest (this requires deleting the
		//manifest-manifest ref first, otherwise the DB will complain)
		clock.StepBy(7 * time.Hour)
		mustExec(t, db1,
			`DELETE FROM manifest_manifest_refs WHERE child_digest = $1`,
			images[2].Manifest.Digest.String(),
		)
		mustExec(t, db1,
			`DELETE FROM manifests WHERE digest = $1`,
			images[2].Manifest.Digest.String(),
		)

		//SyncManifestsInNextRepo should now complain since it wants to delete
		//images[2].Manifest, but it can't because of the manifest-manifest ref to
		//the image list
		expectedError := fmt.Sprintf(`while syncing manifests in a replica repo: cannot remove deleted manifests [%s] in repo test1/foo because they are still being referenced by other manifests (this smells like an inconsistency on the primary account)`,
			images[2].Manifest.Digest.String(),
		)
		expectError(t, expectedError, j2.SyncManifestsInNextRepo())
		//the tag sync went through though, so the tag should be gone (the manifest
		//validation is because of the "other" tag that still exists)
		manifestValidationBecauseOfExistingTag = fmt.Sprintf(
			//this validation is skipped in "on_first_use" because the respective tag is unchanged
			`UPDATE manifests SET validated_at = %d WHERE repo_id = 1 AND digest = '%s';`+"\n",
			clock.Now().Unix(), images[1].Manifest.Digest.String(),
		)
		if strategy == "on_first_use" {
			manifestValidationBecauseOfExistingTag = ""
		}
		tr.DBChanges().AssertEqualf(`%sDELETE FROM tags WHERE repo_id = 1 AND name = 'latest';`,
			manifestValidationBecauseOfExistingTag,
		)

		//also remove the image list manifest on the primary side
		clock.StepBy(7 * time.Hour)
		mustExec(t, db1,
			`DELETE FROM manifests WHERE digest = $1`,
			imageList.Manifest.Digest.String(),
		)
		//and remove the other tag (this is required for the 404 error message in the next step but one to be deterministic)
		mustExec(t, db1, `DELETE FROM tags`)

		//this makes the primary side consistent again, so SyncManifestsInNextRepo
		//should succeed now and remove both deleted manifests from the DB
		expectSuccess(t, j2.SyncManifestsInNextRepo())
		tr.DBChanges().AssertEqualf(`
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[1]s' AND blob_id = 4;
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[1]s' AND blob_id = 5;
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[1]s' AND blob_id = 6;
			DELETE FROM manifest_contents WHERE repo_id = 1 AND digest = '%[1]s';
			DELETE FROM manifest_contents WHERE repo_id = 1 AND digest = '%[2]s';
			DELETE FROM manifest_manifest_refs WHERE repo_id = 1 AND parent_digest = '%[2]s' AND child_digest = '%[3]s';
			DELETE FROM manifest_manifest_refs WHERE repo_id = 1 AND parent_digest = '%[2]s' AND child_digest = '%[1]s';
			DELETE FROM manifests WHERE repo_id = 1 AND digest = '%[1]s';
			DELETE FROM manifests WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE repos SET next_manifest_sync_at = %[4]d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
			DELETE FROM tags WHERE repo_id = 1 AND name = 'other';
		`,
			images[2].Manifest.Digest.String(),
			imageList.Manifest.Digest.String(),
			images[1].Manifest.Digest.String(),
			clock.Now().Add(1*time.Hour).Unix(),
		)
		expectError(t, sql.ErrNoRows.Error(), j2.SyncManifestsInNextRepo())
		tr.DBChanges().AssertEmpty()

		//replace the primary registry's API with something that just answers 404 all the time
		clock.StepBy(7 * time.Hour)
		http.DefaultClient.Transport.(*test.RoundTripper).Handlers["registry.example.org"] = http.HandlerFunc(answerWith404)
		//This is particularly devious since 404 is returned by the GET endpoint for
		//a manifest when the manifest was deleted. We want to check that the next
		//SyncManifestsInNextRepo understands that this is a network issue and not
		//caused by the manifest getting deleted, since the 404-generating endpoint
		//does not render a proper MANIFEST_UNKNOWN error.
		expectedError = fmt.Sprintf(`while syncing manifests in a replica repo: cannot check existence of manifest test1/foo/%s on primary account: during GET https://registry.example.org/v2/test1/foo/manifests/%[1]s: expected status 200, but got 404 Not Found`,
			images[1].Manifest.Digest.String(), //the only manifest that is left
		)
		expectError(t, expectedError, j2.SyncManifestsInNextRepo())
		tr.DBChanges().AssertEmpty()

		//check that the manifest sync did not update the last_pulled_at timestamps
		//in the primary DB (even though there were GET requests for the manifests
		//there)
		var lastPulledAt time.Time
		expectSuccess(t, db1.DbMap.QueryRow(`SELECT MAX(last_pulled_at) FROM manifests`).Scan(&lastPulledAt))
		if !lastPulledAt.Equal(initialLastPulledAt) {
			t.Error("last_pulled_at timestamps on the primary side were touched")
			t.Logf("  expected = %#v", initialLastPulledAt)
			t.Logf("  actual   = %#v", lastPulledAt)
		}

		//flip back to the actual primary registry's API
		http.DefaultClient.Transport.(*test.RoundTripper).Handlers["registry.example.org"] = h1
		//delete the entire repository on the primary
		clock.StepBy(7 * time.Hour)
		mustExec(t, db1, `DELETE FROM manifests`)
		mustExec(t, db1, `DELETE FROM repos`)
		//the manifest sync should reflect the repository deletion on the replica
		expectSuccess(t, j2.SyncManifestsInNextRepo())
		tr.DBChanges().AssertEqualf(`
			DELETE FROM blob_mounts WHERE blob_id = 1 AND repo_id = 1;
			DELETE FROM blob_mounts WHERE blob_id = 2 AND repo_id = 1;
			DELETE FROM blob_mounts WHERE blob_id = 3 AND repo_id = 1;
			DELETE FROM blob_mounts WHERE blob_id = 4 AND repo_id = 1;
			DELETE FROM blob_mounts WHERE blob_id = 5 AND repo_id = 1;
			DELETE FROM blob_mounts WHERE blob_id = 6 AND repo_id = 1;
			DELETE FROM blob_mounts WHERE blob_id = 7 AND repo_id = 1;
			DELETE FROM blob_mounts WHERE blob_id = 8 AND repo_id = 1;
			DELETE FROM blob_mounts WHERE blob_id = 9 AND repo_id = 1;
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[1]s' AND blob_id = 1;
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[1]s' AND blob_id = 2;
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[1]s' AND blob_id = 3;
			DELETE FROM manifest_contents WHERE repo_id = 1 AND digest = '%[1]s';
			DELETE FROM manifests WHERE repo_id = 1 AND digest = '%[1]s';
			DELETE FROM repos WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
		`,
			images[1].Manifest.Digest.String(),
		)
		expectError(t, sql.ErrNoRows.Error(), j2.SyncManifestsInNextRepo())
		tr.DBChanges().AssertEmpty()
	})
}

func answerWith404(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not found", http.StatusNotFound)
}

////////////////////////////////////////////////////////////////////////////////
// tests for CheckVulnerabilitiesForNextManifest

func TestCheckVulnerabilitiesForNextManifest(t *testing.T) {
	j, _, db, _, sd, clock, h := setup(t)
	clock.StepBy(1 * time.Hour)

	//setup two image manifests with just one content layer (we don't really care about
	//the content since our Clair double doesn't care either)
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

	tr, tr0 := easypg.NewTracker(t, db.DbMap.Db)
	tr0.AssertEqualToFile("fixtures/vulnerability-check-setup.sql")

	//setup our Clair API double
	claird := test.NewClairDouble()
	tt := &test.RoundTripper{
		Handlers: map[string]http.Handler{
			"registry.example.org": h,
			"clair.example.org":    api.Compose(claird),
		},
	}
	http.DefaultClient.Transport = tt
	j.cfg.ClairClient = &clair.Client{
		BaseURL:      mustParseURL("https://clair.example.org/"),
		PresharedKey: []byte("doesnotmatter"), //since the ClairDouble does not check the Authorization header
	}

	//ClairDouble wants to know which image manifests to expect (only the
	//non-list manifests are relevant here; the list manifest does not contain
	//any blobs and thus only aggregates its submanifests' vulnerability
	//statuses)
	for idx, image := range images {
		claird.IndexFixtures[image.Manifest.Digest.String()] = fmt.Sprintf("fixtures/clair/manifest-%03d.json", idx+1)
	}
	//Clair support currently requires a storage driver that can do URLForBlob()
	sd.(*test.StorageDriver).AllowDummyURLs = true

	//first round of CheckVulnerabilitiesForNextManifest should submit manifests
	//to Clair for indexing, but since Clair is not done indexing yet, images
	//stay in vulnerability status "Pending" for now
	clock.StepBy(30 * time.Minute)
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest()) //once for each manifest
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest())
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest())
	expectError(t, sql.ErrNoRows.Error(), j.CheckVulnerabilitiesForNextManifest())
	tr.DBChanges().AssertEqual(`
		UPDATE manifests SET next_vuln_check_at = 5520 WHERE repo_id = 1 AND digest = 'sha256:7c5ed02bcdf0dbddf6f1664e01d6a1505c880e296a599371eb919e0e053c0aef';
		UPDATE manifests SET next_vuln_check_at = 5520 WHERE repo_id = 1 AND digest = 'sha256:be414f354c95cb5c3e26d604f5fc79523c68c3f86e0fae98060d5bbc8db466c3';
		UPDATE manifests SET next_vuln_check_at = 5520 WHERE repo_id = 1 AND digest = 'sha256:dbed29ef114646eb4018436b03c6081f63e8a2693a78e3557b0cd240494fa3c0';
	`)

	//five minutes later, indexing is still not finished
	clock.StepBy(5 * time.Minute)
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest()) //once for each manifest
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest())
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest())
	expectError(t, sql.ErrNoRows.Error(), j.CheckVulnerabilitiesForNextManifest())
	tr.DBChanges().AssertEqual(`
		UPDATE manifests SET next_vuln_check_at = 5820 WHERE repo_id = 1 AND digest = 'sha256:7c5ed02bcdf0dbddf6f1664e01d6a1505c880e296a599371eb919e0e053c0aef';
		UPDATE manifests SET next_vuln_check_at = 5820 WHERE repo_id = 1 AND digest = 'sha256:be414f354c95cb5c3e26d604f5fc79523c68c3f86e0fae98060d5bbc8db466c3';
		UPDATE manifests SET next_vuln_check_at = 5820 WHERE repo_id = 1 AND digest = 'sha256:dbed29ef114646eb4018436b03c6081f63e8a2693a78e3557b0cd240494fa3c0';
	`)

	//five minutes later, indexing is finished now and ClairDouble provides vulnerability reports to us
	claird.ReportFixtures[images[0].Manifest.Digest.String()] = "fixtures/clair/report-vulnerable.json"
	claird.ReportFixtures[images[1].Manifest.Digest.String()] = "fixtures/clair/report-clean.json"
	clock.StepBy(5 * time.Minute)
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest()) //once for each manifest
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest())
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest())
	expectError(t, sql.ErrNoRows.Error(), j.CheckVulnerabilitiesForNextManifest())
	tr.DBChanges().AssertEqual(`
		UPDATE manifests SET next_vuln_check_at = 9600, vuln_status = 'Low' WHERE repo_id = 1 AND digest = 'sha256:7c5ed02bcdf0dbddf6f1664e01d6a1505c880e296a599371eb919e0e053c0aef';
		UPDATE manifests SET next_vuln_check_at = 9600, vuln_status = 'Clean' WHERE repo_id = 1 AND digest = 'sha256:be414f354c95cb5c3e26d604f5fc79523c68c3f86e0fae98060d5bbc8db466c3';
		UPDATE manifests SET next_vuln_check_at = 9600, vuln_status = 'Low' WHERE repo_id = 1 AND digest = 'sha256:dbed29ef114646eb4018436b03c6081f63e8a2693a78e3557b0cd240494fa3c0';
	`)
}

func mustParseURL(in string) url.URL {
	u, err := url.Parse(in)
	if err != nil {
		panic(err.Error())
	}
	return *u
}
