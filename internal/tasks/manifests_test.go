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

	"github.com/sapcc/go-bits/assert"
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
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)

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
		allBlobIDs = append(allBlobIDs,
			image.Layers[0].MustUpload(t, s, fooRepoRef).ID,
			image.Layers[1].MustUpload(t, s, fooRepoRef).ID,
			image.Config.MustUpload(t, s, fooRepoRef).ID,
		)

		images[idx] = image
		image.MustUpload(t, s, fooRepoRef, "")
		allManifestDigests = append(allManifestDigests, image.Manifest.Digest.String())
	}

	//also setup an image list manifest containing those images (so that we have
	//some manifest-manifest refs to play with)
	imageList := test.GenerateImageList(images[0], images[1])
	imageList.MustUpload(t, s, fooRepoRef, "")
	allManifestDigests = append(allManifestDigests, imageList.Manifest.Digest.String())

	//since these manifests were just uploaded, validated_at is set to right now,
	//so ValidateNextManifest will report that there is nothing to do
	expectError(t, sql.ErrNoRows.Error(), j.ValidateNextManifest())

	//once they need validating, they validate successfully
	s.Clock.StepBy(36 * time.Hour)
	expectSuccess(t, j.ValidateNextManifest())
	expectSuccess(t, j.ValidateNextManifest())
	expectSuccess(t, j.ValidateNextManifest())
	expectError(t, sql.ErrNoRows.Error(), j.ValidateNextManifest())
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/manifest-validate-001-before-disturbance.sql")

	//disturb the DB state, then rerun ValidateNextManifest to fix it
	s.Clock.StepBy(36 * time.Hour)
	disturb(s.DB, allBlobIDs, allManifestDigests)
	expectSuccess(t, j.ValidateNextManifest())
	expectSuccess(t, j.ValidateNextManifest())
	expectSuccess(t, j.ValidateNextManifest())
	expectError(t, sql.ErrNoRows.Error(), j.ValidateNextManifest())
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/manifest-validate-002-after-fix.sql")
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
	j, s := setup(t)

	//setup a manifest that is missing a referenced blob (we need to do this
	//manually since the MustUpload functions care about uploading stuff intact)
	s.Clock.StepBy(1 * time.Hour)
	image := test.GenerateImage( /* no layers */ )
	must(t, s.DB.Insert(&keppel.Manifest{
		RepositoryID:        1,
		Digest:              image.Manifest.Digest.String(),
		MediaType:           image.Manifest.MediaType,
		SizeBytes:           image.SizeBytes(),
		PushedAt:            s.Clock.Now(),
		ValidatedAt:         s.Clock.Now(),
		VulnerabilityStatus: clair.PendingVulnerabilityStatus,
	}))
	must(t, s.DB.Insert(&keppel.ManifestContent{
		RepositoryID: 1,
		Digest:       image.Manifest.Digest.String(),
		Content:      image.Manifest.Contents,
	}))
	must(t, s.SD.WriteManifest(*s.Accounts[0], "foo", image.Manifest.Digest.String(), image.Manifest.Contents))

	//validation should yield an error
	s.Clock.StepBy(36 * time.Hour)
	expectedError := "while validating a manifest: manifest blob unknown to registry: " + image.Config.Digest.String()
	expectError(t, expectedError, j.ValidateNextManifest())

	//check that validation error to be recorded in the DB
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/manifest-validate-error-001.sql")

	//expect next ValidateNextManifest run to skip over this manifest since it
	//was recently validated
	expectError(t, sql.ErrNoRows.Error(), j.ValidateNextManifest())

	//upload missing blob so that we can test recovering from the validation error
	image.Config.MustUpload(t, s, fooRepoRef)

	//next validation should be happy (and also create the missing refs)
	s.Clock.StepBy(36 * time.Hour)
	expectSuccess(t, j.ValidateNextManifest())
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/manifest-validate-error-002.sql")
}

////////////////////////////////////////////////////////////////////////////////
// tests for SyncManifestsInNextRepo

func TestSyncManifestsInNextRepo(t *testing.T) {
	forAllReplicaTypes(t, func(strategy string) {
		test.WithRoundTripper(func(tt *test.RoundTripper) {
			j1, s1 := setup(t)
			j2, s2 := setupReplica(t, s1, strategy)
			s1.Clock.StepBy(1 * time.Hour)
			replicaToken := s2.GetToken(t, "repository:test1/foo:pull")

			//upload some manifests...
			images := make([]test.Image, 4)
			for idx := range images {
				image := test.GenerateImage(
					test.GenerateExampleLayer(int64(10*idx+1)),
					test.GenerateExampleLayer(int64(10*idx+2)),
				)
				images[idx] = image

				//...to the primary account...
				image.MustUpload(t, s1, fooRepoRef, "")

				//...and most of them also to the replica account (to simulate replication having taken place)
				if idx != 0 {
					assert.HTTPRequest{
						Method:       "GET",
						Path:         fmt.Sprintf("/v2/test1/foo/manifests/%s", image.Manifest.Digest.String()),
						Header:       map[string]string{"Authorization": "Bearer " + replicaToken},
						ExpectStatus: http.StatusOK,
						ExpectBody:   assert.ByteData(image.Manifest.Contents),
					}.Check(t, s2.Handler)
				}
			}

			//some of the replicated images are also tagged
			for _, db := range []*keppel.DB{s1.DB, s2.DB} {
				for _, tagName := range []string{"latest", "other"} {
					mustExec(t, db,
						`INSERT INTO tags (repo_id, name, digest, pushed_at) VALUES (1, $1, $2, $3)`,
						tagName,
						images[1].Manifest.Digest.String(),
						s1.Clock.Now(),
					)
				}
			}

			//also setup an image list manifest containing some of those images (so that we have
			//some manifest-manifest refs to play with)
			imageList := test.GenerateImageList(images[1], images[2])
			imageList.MustUpload(t, s1, fooRepoRef, "")
			//this one is replicated as well
			assert.HTTPRequest{
				Method:       "GET",
				Path:         fmt.Sprintf("/v2/test1/foo/manifests/%s", imageList.Manifest.Digest.String()),
				Header:       map[string]string{"Authorization": "Bearer " + replicaToken},
				ExpectStatus: http.StatusOK,
				ExpectBody:   assert.ByteData(imageList.Manifest.Contents),
			}.Check(t, s2.Handler)

			//set a well-known last_pulled_at timestamp on all manifests in the primary
			//DB (we will later verify that this was not touched by the manifest sync)
			initialLastPulledAt := time.Unix(42, 0)
			mustExec(t, s1.DB, `UPDATE manifests SET last_pulled_at = $1`, initialLastPulledAt)
			mustExec(t, s1.DB, `UPDATE tags SET last_pulled_at = $1`, initialLastPulledAt)
			//we set last_pulled_at to NULL on images[3] to verify that we can merge
			//NULL with a non-NULL last_pulled_at from the replica side
			mustExec(t, s1.DB, `UPDATE manifests SET last_pulled_at = NULL WHERE digest = $1`, images[3].Manifest.Digest.String())

			//as an exception, in the on_first_use method, we can and want to merge
			//last_pulled_at timestamps from the replica into those of the primary, so
			//set some of those to verify the merging behavior
			earlierLastPulledAt := initialLastPulledAt.Add(-10 * time.Second)
			laterLastPulledAt := initialLastPulledAt.Add(+10 * time.Second)
			mustExec(t, s2.DB, `UPDATE manifests SET last_pulled_at = NULL`)
			mustExec(t, s2.DB, `UPDATE tags SET last_pulled_at = NULL`)
			mustExec(t, s2.DB, `UPDATE manifests SET last_pulled_at = $1 WHERE digest = $2`, earlierLastPulledAt, images[1].Manifest.Digest.String())
			mustExec(t, s2.DB, `UPDATE manifests SET last_pulled_at = $1 WHERE digest = $2`, laterLastPulledAt, images[2].Manifest.Digest.String())
			mustExec(t, s2.DB, `UPDATE manifests SET last_pulled_at = $1 WHERE digest = $2`, initialLastPulledAt, images[3].Manifest.Digest.String())
			mustExec(t, s2.DB, `UPDATE tags SET last_pulled_at = $1 WHERE name = $2`, earlierLastPulledAt, "latest")
			mustExec(t, s2.DB, `UPDATE tags SET last_pulled_at = $1 WHERE name = $2`, laterLastPulledAt, "other")

			tr, tr0 := easypg.NewTracker(t, s2.DB.DbMap.Db)
			tr0.AssertEqualToFile(fmt.Sprintf("fixtures/manifest-sync-setup-%s.sql", strategy))
			trForPrimary, _ := easypg.NewTracker(t, s1.DB.DbMap.Db)

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
				s1.Clock.Now().Add(1*time.Hour).Unix(),
			)
			//second run should not have anything else to do
			expectError(t, sql.ErrNoRows.Error(), j2.SyncManifestsInNextRepo())
			tr.DBChanges().AssertEmpty()

			//in on_first_use, the sync should have merged the replica's last_pulled_at
			//timestamps into the primary, i.e. primary.last_pulled_at =
			//max(primary.last_pulled_at, replica.last_pulled_at); this only touches
			//the DB when the replica's last_pulled_at is after the primary's
			if strategy == "on_first_use" {
				trForPrimary.DBChanges().AssertEqualf(`
						UPDATE manifests SET last_pulled_at = %[1]d WHERE repo_id = 1 AND digest = '%[2]s';
						UPDATE manifests SET last_pulled_at = %[3]d WHERE repo_id = 1 AND digest = '%[4]s';
						UPDATE tags SET last_pulled_at = %[3]d WHERE repo_id = 1 AND name = 'other';
					`,
					initialLastPulledAt.Unix(),
					images[3].Manifest.Digest.String(),
					laterLastPulledAt.Unix(),
					images[2].Manifest.Digest.String(),
				)
				//reset all timestamps to prevent divergences in the rest of the test
				mustExec(t, s1.DB, `UPDATE manifests SET last_pulled_at = $1`, initialLastPulledAt)
				mustExec(t, s1.DB, `UPDATE tags SET last_pulled_at = $1`, initialLastPulledAt)
				mustExec(t, s2.DB, `UPDATE manifests SET last_pulled_at = $1`, initialLastPulledAt)
				mustExec(t, s2.DB, `UPDATE tags SET last_pulled_at = $1`, initialLastPulledAt)
				tr.DBChanges() // skip these changes
			} else {
				trForPrimary.DBChanges().AssertEmpty()
			}

			//delete a manifest on the primary side (this one is a simple image not referenced by anyone else)
			s1.Clock.StepBy(2 * time.Hour)
			mustExec(t, s1.DB,
				`DELETE FROM manifests WHERE digest = $1`,
				images[3].Manifest.Digest.String(),
			)
			//move a tag on the primary side
			mustExec(t, s1.DB,
				`UPDATE tags SET digest = $1 WHERE name = 'latest'`,
				images[2].Manifest.Digest.String(),
			)

			//again, nothing to do on the primary side
			expectError(t, sql.ErrNoRows.Error(), j1.SyncManifestsInNextRepo())
			//SyncManifestsInNextRepo on the replica side should not do anything while
			//the account is in maintenance; only the timestamp is updated to make sure
			//that the job loop progresses to the next repo
			mustExec(t, s2.DB, `UPDATE accounts SET in_maintenance = TRUE`)
			expectSuccess(t, j2.SyncManifestsInNextRepo())
			tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET in_maintenance = TRUE WHERE name = 'test1';
			UPDATE repos SET next_manifest_sync_at = %d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
		`,
				s1.Clock.Now().Add(1*time.Hour).Unix(),
			)
			expectError(t, sql.ErrNoRows.Error(), j2.SyncManifestsInNextRepo())
			tr.DBChanges().AssertEmpty()

			//end maintenance
			mustExec(t, s2.DB, `UPDATE accounts SET in_maintenance = FALSE`)
			tr.DBChanges().AssertEqual(`UPDATE accounts SET in_maintenance = FALSE WHERE name = 'test1';`)

			//test that replication from external uses the inbound cache
			if strategy == "from_external_on_first_use" {
				//after the end of the maintenance, we would naively expect
				//SyncManifestsInNextRepo to actually replicate the deletion, BUT we have an
				//inbound cache with a lifetime of 6 hours, so actually nothing should
				//happen (only the tag gets synced, which includes a validation of the
				//referenced manifest)
				s1.Clock.StepBy(2 * time.Hour)
				expectSuccess(t, j2.SyncManifestsInNextRepo())
				tr.DBChanges().AssertEqualf(`
			UPDATE manifests SET validated_at = %d WHERE repo_id = 1 AND digest = '%s';
			UPDATE repos SET next_manifest_sync_at = %d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
		`,
					s1.Clock.Now().Unix(),
					images[1].Manifest.Digest.String(),
					s1.Clock.Now().Add(1*time.Hour).Unix(),
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
			s1.Clock.StepBy(7 * time.Hour)
			expectSuccess(t, j2.SyncManifestsInNextRepo())
			manifestValidationBecauseOfExistingTag := fmt.Sprintf(
				//this validation is skipped in "on_first_use" because the respective tag is unchanged
				`UPDATE manifests SET validated_at = %d WHERE repo_id = 1 AND digest = '%s';`+"\n",
				s1.Clock.Now().Unix(), images[1].Manifest.Digest.String(),
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
				s1.Clock.Now().Unix(),
				images[2].Manifest.Digest.String(), //the manifest now tagged as "latest"
				s1.Clock.Now().Add(1*time.Hour).Unix(),
				manifestValidationBecauseOfExistingTag,
			)
			expectError(t, sql.ErrNoRows.Error(), j2.SyncManifestsInNextRepo())
			tr.DBChanges().AssertEmpty()

			//cause a deliberate inconsistency on the primary side: delete a manifest that
			//*is* referenced by another manifest (this requires deleting the
			//manifest-manifest ref first, otherwise the DB will complain)
			s1.Clock.StepBy(7 * time.Hour)
			mustExec(t, s1.DB,
				`DELETE FROM manifest_manifest_refs WHERE child_digest = $1`,
				images[2].Manifest.Digest.String(),
			)
			mustExec(t, s1.DB,
				`DELETE FROM manifests WHERE digest = $1`,
				images[2].Manifest.Digest.String(),
			)

			//SyncManifestsInNextRepo should now complain since it wants to delete
			//images[2].Manifest, but it can't because of the manifest-manifest ref to
			//the image list
			expectedError := fmt.Sprintf(`while syncing manifests in the replica repo test1/foo: cannot remove deleted manifests [%s] in repo test1/foo because they are still being referenced by other manifests (this smells like an inconsistency on the primary account)`,
				images[2].Manifest.Digest.String(),
			)
			expectError(t, expectedError, j2.SyncManifestsInNextRepo())
			//the tag sync went through though, so the tag should be gone (the manifest
			//validation is because of the "other" tag that still exists)
			manifestValidationBecauseOfExistingTag = fmt.Sprintf(
				//this validation is skipped in "on_first_use" because the respective tag is unchanged
				`UPDATE manifests SET validated_at = %d WHERE repo_id = 1 AND digest = '%s';`+"\n",
				s1.Clock.Now().Unix(), images[1].Manifest.Digest.String(),
			)
			if strategy == "on_first_use" {
				manifestValidationBecauseOfExistingTag = ""
			}
			tr.DBChanges().AssertEqualf(`%sDELETE FROM tags WHERE repo_id = 1 AND name = 'latest';`,
				manifestValidationBecauseOfExistingTag,
			)

			//also remove the image list manifest on the primary side
			s1.Clock.StepBy(7 * time.Hour)
			mustExec(t, s1.DB,
				`DELETE FROM manifests WHERE digest = $1`,
				imageList.Manifest.Digest.String(),
			)
			//and remove the other tag (this is required for the 404 error message in the next step but one to be deterministic)
			mustExec(t, s1.DB, `DELETE FROM tags`)

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
				s1.Clock.Now().Add(1*time.Hour).Unix(),
			)
			expectError(t, sql.ErrNoRows.Error(), j2.SyncManifestsInNextRepo())
			tr.DBChanges().AssertEmpty()

			//replace the primary registry's API with something that just answers 404 most of the time
			//
			//(We do allow the /keppel/v1/auth endpoint to work properly because
			//otherwise the error messages are not reproducible between passes.)
			s1.Clock.StepBy(7 * time.Hour)
			http.DefaultClient.Transport.(*test.RoundTripper).Handlers["registry.example.org"] = answerMostWith404(s1.Handler)
			//This is particularly devious since 404 is returned by the GET endpoint for
			//a manifest when the manifest was deleted. We want to check that the next
			//SyncManifestsInNextRepo understands that this is a network issue and not
			//caused by the manifest getting deleted, since the 404-generating endpoint
			//does not render a proper MANIFEST_UNKNOWN error.
			expectedError = fmt.Sprintf(`while syncing manifests in the replica repo test1/foo: cannot check existence of manifest test1/foo/%s on primary account: during GET https://registry.example.org/v2/test1/foo/manifests/%[1]s: expected status 200, but got 404 Not Found`,
				images[1].Manifest.Digest.String(), //the only manifest that is left
			)
			expectError(t, expectedError, j2.SyncManifestsInNextRepo())
			tr.DBChanges().AssertEmpty()

			//check that the manifest sync did not update the last_pulled_at timestamps
			//in the primary DB (even though there were GET requests for the manifests
			//there)
			var lastPulledAt time.Time
			expectSuccess(t, s1.DB.DbMap.QueryRow(`SELECT MAX(last_pulled_at) FROM manifests`).Scan(&lastPulledAt))
			if !lastPulledAt.Equal(initialLastPulledAt) {
				t.Error("last_pulled_at timestamps on the primary side were touched")
				t.Logf("  expected = %#v", initialLastPulledAt)
				t.Logf("  actual   = %#v", lastPulledAt)
			}

			//flip back to the actual primary registry's API
			http.DefaultClient.Transport.(*test.RoundTripper).Handlers["registry.example.org"] = s1.Handler
			//delete the entire repository on the primary
			s1.Clock.StepBy(7 * time.Hour)
			mustExec(t, s1.DB, `DELETE FROM manifests`)
			mustExec(t, s1.DB, `DELETE FROM repos`)
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
	})
}

func answerMostWith404(h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/keppel/v1/auth" {
			h.ServeHTTP(w, r)
		} else {
			http.Error(w, "not found", http.StatusNotFound)
		}
	}
}

////////////////////////////////////////////////////////////////////////////////
// tests for CheckVulnerabilitiesForNextManifest

func TestCheckVulnerabilitiesForNextManifest(t *testing.T) {
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)

	//setup two image manifests with just one content layer (we don't really care about
	//the content since our Clair double doesn't care either)
	images := make([]test.Image, 3)
	for idx := range images {
		images[idx] = test.GenerateImage(test.GenerateExampleLayer(int64(idx)))
		images[idx].MustUpload(t, s, fooRepoRef, "")
	}

	//also setup an image list manifest containing those images (so that we have
	//some manifest-manifest refs to play with)
	imageList := test.GenerateImageList(images[0], images[1])
	imageList.MustUpload(t, s, fooRepoRef, "")

	//fake manifest size to check if to big ones (here 10 GiB) are rejected
	mustExec(t, s.DB, `UPDATE manifests SET size_bytes = 10737418240 where digest = 'sha256:a1efa53bd4bbcc4878997c775688438b8ccfd29ccf71f110296dc62d5dabc42d'`)

	tr, tr0 := easypg.NewTracker(t, s.DB.DbMap.Db)
	tr0.AssertEqualToFile("fixtures/vulnerability-check-setup.sql")

	//setup our Clair API double (TODO use test.WithClairDouble instead)
	claird := test.NewClairDouble()
	tt := &test.RoundTripper{
		Handlers: map[string]http.Handler{
			"registry.example.org": s.Handler,
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
	s.SD.AllowDummyURLs = true

	//first round of CheckVulnerabilitiesForNextManifest should submit manifests
	//to Clair for indexing, but since Clair is not done indexing yet, images
	//stay in vulnerability status "Pending" for now
	s.Clock.StepBy(30 * time.Minute)
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest()) //once for each manifest
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest())
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest())
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest())
	expectError(t, sql.ErrNoRows.Error(), j.CheckVulnerabilitiesForNextManifest())
	tr.DBChanges().AssertEqual(`
		UPDATE manifests SET next_vuln_check_at = 5520 WHERE repo_id = 1 AND digest = 'sha256:7c5ed02bcdf0dbddf6f1664e01d6a1505c880e296a599371eb919e0e053c0aef';
		UPDATE manifests SET next_vuln_check_at = 9000, vuln_status = 'Unsupported', vuln_scan_error = 'vulnerability scanning is not supported for images above 5 GiB' WHERE repo_id = 1 AND digest = 'sha256:a1efa53bd4bbcc4878997c775688438b8ccfd29ccf71f110296dc62d5dabc42d';
		UPDATE manifests SET next_vuln_check_at = 5520 WHERE repo_id = 1 AND digest = 'sha256:be414f354c95cb5c3e26d604f5fc79523c68c3f86e0fae98060d5bbc8db466c3';
		UPDATE manifests SET next_vuln_check_at = 5520 WHERE repo_id = 1 AND digest = 'sha256:dbed29ef114646eb4018436b03c6081f63e8a2693a78e3557b0cd240494fa3c0';
	`)

	//five minutes later, indexing is still not finished
	s.Clock.StepBy(5 * time.Minute)
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
	s.Clock.StepBy(5 * time.Minute)
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
