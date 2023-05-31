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
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/jobloop"
	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/clair"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

////////////////////////////////////////////////////////////////////////////////
// tests for ValidateNextManifest

// Base behavior for various unit tests that start with the same image list, destroy
// it in various ways, and check that ValidateNextManifest correctly fixes it.
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
	mustDo(t, s.DB.Insert(&keppel.Manifest{
		RepositoryID: 1,
		Digest:       image.Manifest.Digest,
		MediaType:    image.Manifest.MediaType,
		SizeBytes:    image.SizeBytes(),
		PushedAt:     s.Clock.Now(),
		ValidatedAt:  s.Clock.Now(),
	}))
	mustDo(t, s.DB.Insert(&keppel.ManifestContent{
		RepositoryID: 1,
		Digest:       image.Manifest.Digest.String(),
		Content:      image.Manifest.Contents,
	}))
	mustDo(t, s.DB.Insert(&keppel.VulnerabilityInfo{
		RepositoryID: 1,
		Digest:       image.Manifest.Digest,
		NextCheckAt:  time.Unix(0, 0),
		Status:       clair.PendingVulnerabilityStatus,
	}))
	mustDo(t, s.DB.Insert(&keppel.TrivySecurityInfo{
		RepositoryID:        1,
		Digest:              image.Manifest.Digest,
		NextCheckAt:         time.Unix(0, 0),
		VulnerabilityStatus: clair.PendingVulnerabilityStatus,
	}))
	mustDo(t, s.SD.WriteManifest(*s.Accounts[0], "foo", image.Manifest.Digest, image.Manifest.Contents))

	//validation should yield an error
	s.Clock.StepBy(36 * time.Hour)
	expectedError := fmt.Sprintf("while validating manifest %s in repo 1: manifest blob unknown to registry: %s", image.Manifest.Digest, image.Config.Digest)
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
						Path:         fmt.Sprintf("/v2/test1/foo/manifests/%s", image.Manifest.Digest),
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
						images[1].Manifest.Digest,
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
				Path:         fmt.Sprintf("/v2/test1/foo/manifests/%s", imageList.Manifest.Digest),
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
			mustExec(t, s1.DB, `UPDATE manifests SET last_pulled_at = NULL WHERE digest = $1`, images[3].Manifest.Digest)

			//as an exception, in the on_first_use method, we can and want to merge
			//last_pulled_at timestamps from the replica into those of the primary, so
			//set some of those to verify the merging behavior
			earlierLastPulledAt := initialLastPulledAt.Add(-10 * time.Second)
			laterLastPulledAt := initialLastPulledAt.Add(+10 * time.Second)
			mustExec(t, s2.DB, `UPDATE manifests SET last_pulled_at = NULL`)
			mustExec(t, s2.DB, `UPDATE tags SET last_pulled_at = NULL`)
			mustExec(t, s2.DB, `UPDATE manifests SET last_pulled_at = $1 WHERE digest = $2`, earlierLastPulledAt, images[1].Manifest.Digest)
			mustExec(t, s2.DB, `UPDATE manifests SET last_pulled_at = $1 WHERE digest = $2`, laterLastPulledAt, images[2].Manifest.Digest)
			mustExec(t, s2.DB, `UPDATE manifests SET last_pulled_at = $1 WHERE digest = $2`, initialLastPulledAt, images[3].Manifest.Digest)
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
					images[3].Manifest.Digest,
					laterLastPulledAt.Unix(),
					images[2].Manifest.Digest,
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
				images[3].Manifest.Digest,
			)
			//move a tag on the primary side
			mustExec(t, s1.DB,
				`UPDATE tags SET digest = $1 WHERE name = 'latest'`,
				images[2].Manifest.Digest,
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
					images[1].Manifest.Digest,
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
				s1.Clock.Now().Unix(), images[1].Manifest.Digest,
			)
			if strategy == "on_first_use" {
				manifestValidationBecauseOfExistingTag = ""
			}
			tr.DBChanges().AssertEqualf(`
					DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[1]s' AND blob_id = 7;
					DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[1]s' AND blob_id = 8;
					DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[1]s' AND blob_id = 9;
					DELETE FROM manifest_contents WHERE repo_id = 1 AND digest = '%[1]s';
					DELETE FROM manifests WHERE repo_id = 1 AND digest = '%[1]s';
					%[5]sUPDATE manifests SET validated_at = %[2]d WHERE repo_id = 1 AND digest = '%[3]s';
					UPDATE repos SET next_manifest_sync_at = %[4]d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
					UPDATE tags SET digest = '%[3]s', pushed_at = %[2]d, last_pulled_at = NULL WHERE repo_id = 1 AND name = 'latest';
					DELETE FROM trivy_security_info WHERE repo_id = 1 AND digest = '%[1]s';
					DELETE FROM vuln_info WHERE repo_id = 1 AND digest = '%[1]s';
				`,
				images[3].Manifest.Digest, //the deleted manifest
				s1.Clock.Now().Unix(),
				images[2].Manifest.Digest, //the manifest now tagged as "latest"
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
				images[2].Manifest.Digest,
			)
			mustExec(t, s1.DB,
				`DELETE FROM manifests WHERE digest = $1`,
				images[2].Manifest.Digest,
			)

			//SyncManifestsInNextRepo should now complain since it wants to delete
			//images[2].Manifest, but it can't because of the manifest-manifest ref to
			//the image list
			expectedError := fmt.Sprintf(`while syncing manifests in the replica repo test1/foo: cannot remove deleted manifests [%s] in repo test1/foo because they are still being referenced by other manifests (this smells like an inconsistency on the primary account)`,
				images[2].Manifest.Digest,
			)
			expectError(t, expectedError, j2.SyncManifestsInNextRepo())
			//the tag sync went through though, so the tag should be gone (the manifest
			//validation is because of the "other" tag that still exists)
			manifestValidationBecauseOfExistingTag = fmt.Sprintf(
				//this validation is skipped in "on_first_use" because the respective tag is unchanged
				`UPDATE manifests SET validated_at = %d WHERE repo_id = 1 AND digest = '%s';`+"\n",
				s1.Clock.Now().Unix(), images[1].Manifest.Digest,
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
				imageList.Manifest.Digest,
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
					DELETE FROM trivy_security_info WHERE repo_id = 1 AND digest = '%[1]s';
					DELETE FROM trivy_security_info WHERE repo_id = 1 AND digest = '%[2]s';
					DELETE FROM vuln_info WHERE repo_id = 1 AND digest = '%[1]s';
					DELETE FROM vuln_info WHERE repo_id = 1 AND digest = '%[2]s';
				`,
				images[2].Manifest.Digest,
				imageList.Manifest.Digest,
				images[1].Manifest.Digest,
				s1.Clock.Now().Add(1*time.Hour).Unix(),
			)
			expectError(t, sql.ErrNoRows.Error(), j2.SyncManifestsInNextRepo())
			tr.DBChanges().AssertEmpty()

			//replace the primary registry's API with something that just answers 404 most of the time
			//
			//(We do allow the /keppel/v1/auth endpoint to work properly because
			//otherwise the error messages are not reproducible between passes.)
			s1.Clock.StepBy(7 * time.Hour)
			http.DefaultTransport.(*test.RoundTripper).Handlers["registry.example.org"] = answerMostWith404(s1.Handler)
			//This is particularly devious since 404 is returned by the GET endpoint for
			//a manifest when the manifest was deleted. We want to check that the next
			//SyncManifestsInNextRepo understands that this is a network issue and not
			//caused by the manifest getting deleted, since the 404-generating endpoint
			//does not render a proper MANIFEST_UNKNOWN error.
			expectedError = fmt.Sprintf(`while syncing manifests in the replica repo test1/foo: cannot check existence of manifest test1/foo/%s on primary account: during GET https://registry.example.org/v2/test1/foo/manifests/%[1]s: expected status 200, but got 404 Not Found`,
				images[1].Manifest.Digest, //the only manifest that is left
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
			http.DefaultTransport.(*test.RoundTripper).Handlers["registry.example.org"] = s1.Handler
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
					DELETE FROM trivy_security_info WHERE repo_id = 1 AND digest = '%[1]s';
					DELETE FROM vuln_info WHERE repo_id = 1 AND digest = '%[1]s';
				`,
				images[1].Manifest.Digest,
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
	test.WithRoundTripper(func(_ *test.RoundTripper) {
		j, s := setup(t, test.WithClairDouble, test.WithTrivyDouble)
		s.Clock.StepBy(1 * time.Hour)

		//setup two image manifests with just one content layer (we don't really care about
		//the content since our Clair double doesn't care either)
		images := make([]test.Image, 3)
		for idx := range images {
			images[idx] = test.GenerateImage(test.GenerateExampleLayer(int64(idx)))
			images[idx].MustUpload(t, s, fooRepoRef, "")
		}
		// generate a 2 MiB big image to run into blobUncompressedSizeTooBigGiB
		images = append(images, test.GenerateImage(test.GenerateExampleLayerSize(int64(2), 2)))
		images[3].MustUpload(t, s, fooRepoRef, "")

		//also setup an image list manifest containing those images (so that we have
		//some manifest-manifest refs to play with)
		imageList := test.GenerateImageList(images[0], images[1])
		imageList.MustUpload(t, s, fooRepoRef, "")

		//fake manifest size to check if to big ones (here 10 GiB) are rejected
		//when uncompressing it is still 1 MiB big those trigger manifestSizeTooBigGiB but not blobUncompressedSizeTooBigGiB
		mustExec(t, s.DB, fmt.Sprintf(`UPDATE manifests SET size_bytes = 10737418240 where digest = '%s'`, imageList.Manifest.Digest))

		//adjust too big values down to make testing easier
		manifestSizeTooBigGiB = 0.002
		blobUncompressedSizeTooBigGiB = 0.001

		tr, tr0 := easypg.NewTracker(t, s.DB.DbMap.Db)
		tr0.AssertEqualToFile("fixtures/vulnerability-check-setup.sql")

		//ClairDouble wants to know which image manifests to expect (only the
		//non-list manifests are relevant here; the list manifest does not contain
		//any blobs and thus only aggregates its submanifests' vulnerability
		//statuses)
		for idx, image := range images {
			s.ClairDouble.IndexFixtures[image.Manifest.Digest] = fmt.Sprintf("fixtures/clair/manifest-%03d.json", idx+1)
		}

		trivyJob := j.CheckTrivySecurityStatusJob(s.Registry)

		//first round of CheckVulnerabilitiesForNextManifest should submit manifests
		//to Clair for indexing, but since Clair is not done indexing yet, images
		//stay in vulnerability status "Pending" for now
		s.Clock.StepBy(30 * time.Minute)
		//once for each manifest
		expectSuccess(t, ExecuteN(j.CheckVulnerabilitiesForNextManifest(), 5))
		expectError(t, sql.ErrNoRows.Error(), ExecuteOne(j.CheckVulnerabilitiesForNextManifest()))
		tr.DBChanges().AssertEqualf(`
			UPDATE blobs SET blocks_vuln_scanning = FALSE WHERE id = 1 AND account_name = 'test1' AND digest = '%[8]s';
			UPDATE blobs SET blocks_vuln_scanning = FALSE WHERE id = 3 AND account_name = 'test1' AND digest = '%[9]s';
			UPDATE blobs SET blocks_vuln_scanning = FALSE WHERE id = 5 AND account_name = 'test1' AND digest = '%[10]s';
			UPDATE blobs SET blocks_vuln_scanning = TRUE WHERE id = 7 AND account_name = 'test1' AND digest = '%[11]s';
			UPDATE vuln_info SET next_check_at = 5520, checked_at = 5400, index_started_at = 5400, index_state = '%[12]s', check_duration_secs = 0 WHERE repo_id = 1 AND digest = '%[4]s';
			UPDATE vuln_info SET status = 'Unsupported', message = 'vulnerability scanning is not supported for images above %[1]g GiB', next_check_at = 91800 WHERE repo_id = 1 AND digest = '%[3]s';
			UPDATE vuln_info SET next_check_at = 5520, checked_at = 5400, index_started_at = 5400, index_state = '%[12]s', check_duration_secs = 0 WHERE repo_id = 1 AND digest = '%[6]s';
			UPDATE vuln_info SET status = 'Unsupported', message = 'vulnerability scanning is not supported for uncompressed image layers above %[2]g GiB', next_check_at = 91800 WHERE repo_id = 1 AND digest = '%[7]s';
			UPDATE vuln_info SET next_check_at = 5520, checked_at = 5400, index_started_at = 5400, index_state = '%[12]s', check_duration_secs = 0 WHERE repo_id = 1 AND digest = '%[5]s';
		`,
			manifestSizeTooBigGiB, blobUncompressedSizeTooBigGiB, imageList.Manifest.Digest,
			images[0].Manifest.Digest, images[1].Manifest.Digest, images[2].Manifest.Digest, images[3].Manifest.Digest,
			images[0].Layers[0].Digest, images[1].Layers[0].Digest, images[2].Layers[0].Digest, images[3].Layers[0].Digest,
			test.IndexStateHash,
		)

		//five minutes later, indexing is still not finished
		s.Clock.StepBy(5 * time.Minute)
		//once for each manifest
		expectSuccess(t, ExecuteN(j.CheckVulnerabilitiesForNextManifest(), 3))
		expectError(t, sql.ErrNoRows.Error(), ExecuteOne(j.CheckVulnerabilitiesForNextManifest()))
		tr.DBChanges().AssertEqualf(`
			UPDATE vuln_info SET next_check_at = 5820, checked_at = 5700 WHERE repo_id = 1 AND digest = '%s';
			UPDATE vuln_info SET next_check_at = 5820, checked_at = 5700 WHERE repo_id = 1 AND digest = '%s';
			UPDATE vuln_info SET next_check_at = 5820, checked_at = 5700 WHERE repo_id = 1 AND digest = '%s';
		`, images[0].Manifest.Digest, images[2].Manifest.Digest, images[1].Manifest.Digest)

		// five minutes later, indexing is finished now and ClairDouble provides vulnerability reports to us
		// trivy is checked here first because it returns result immediately and the above code will be removed when clair support is removed
		s.ClairDouble.ReportFixtures[images[0].Manifest.Digest] = "fixtures/clair/report-vulnerable.json"
		s.ClairDouble.ReportFixtures[images[1].Manifest.Digest] = "fixtures/clair/report-clean.json"
		s.TrivyDouble.ReportFixtures[imageList.ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-vulnerable.json"
		s.TrivyDouble.ReportFixtures[images[0].ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-vulnerable.json"
		s.TrivyDouble.ReportFixtures[images[1].ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-clean.json"
		s.TrivyDouble.ReportFixtures[images[2].ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-vulnerable.json"
		s.TrivyDouble.ReportFixtures[images[3].ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-clean.json"
		s.Clock.StepBy(5 * time.Minute)
		//once for each manifest
		expectSuccess(t, ExecuteN(j.CheckVulnerabilitiesForNextManifest(), 3))
		expectSuccess(t, jobloop.ProcessMany(trivyJob, s.Ctx, 5))
		expectError(t, sql.ErrNoRows.Error(), ExecuteOne(j.CheckVulnerabilitiesForNextManifest()))
		expectError(t, sql.ErrNoRows.Error(), trivyJob.ProcessOne(s.Ctx))
		tr.DBChanges().AssertEqualf(`
			UPDATE trivy_security_info SET vuln_status = 'Critical', next_check_at = 9600, checked_at = 6000, check_duration_secs = 0 WHERE repo_id = 1 AND digest = '%s';
			UPDATE trivy_security_info SET vuln_status = 'Critical', next_check_at = 9600, checked_at = 6000, check_duration_secs = 0 WHERE repo_id = 1 AND digest = '%s';
			UPDATE trivy_security_info SET vuln_status = 'Critical', next_check_at = 9600, checked_at = 6000, check_duration_secs = 0 WHERE repo_id = 1 AND digest = '%s';
			UPDATE trivy_security_info SET vuln_status = 'Unsupported', message = 'vulnerability scanning is not supported for uncompressed image layers above 0.001 GiB', next_check_at = 92400 WHERE repo_id = 1 AND digest = '%s';
			UPDATE trivy_security_info SET vuln_status = 'Clean', next_check_at = 9600, checked_at = 6000, check_duration_secs = 0 WHERE repo_id = 1 AND digest = '%s';
			UPDATE vuln_info SET status = 'Low', next_check_at = 9600, checked_at = 6000, index_finished_at = 6000 WHERE repo_id = 1 AND digest = '%s';
			UPDATE vuln_info SET next_check_at = 6120, checked_at = 6000 WHERE repo_id = 1 AND digest = '%s';
			UPDATE vuln_info SET status = 'Clean', next_check_at = 9600, checked_at = 6000, index_finished_at = 6000 WHERE repo_id = 1 AND digest = '%s';
		`, images[0].Manifest.Digest, imageList.Manifest.Digest, images[2].Manifest.Digest, images[3].Manifest.Digest, images[1].Manifest.Digest,
			images[0].Manifest.Digest, images[2].Manifest.Digest, images[1].Manifest.Digest)

		// check that a changed vulnerability status does not have side effects
		s.ClairDouble.ReportFixtures[images[1].Manifest.Digest] = "fixtures/clair/report-vulnerable.json"
		s.TrivyDouble.ReportFixtures[images[1].ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-vulnerable.json"
		s.Clock.StepBy(1 * time.Hour)
		//once for each manifest
		expectSuccess(t, ExecuteN(j.CheckVulnerabilitiesForNextManifest(), 3))
		expectSuccess(t, jobloop.ProcessMany(trivyJob, s.Ctx, 4))
		expectError(t, sql.ErrNoRows.Error(), ExecuteOne(j.CheckVulnerabilitiesForNextManifest()))
		expectError(t, sql.ErrNoRows.Error(), trivyJob.ProcessOne(s.Ctx))
		tr.DBChanges().AssertEqualf(`
			UPDATE trivy_security_info SET next_check_at = 13200, checked_at = 9600 WHERE repo_id = 1 AND digest = '%s';
			UPDATE trivy_security_info SET next_check_at = 13200, checked_at = 9600 WHERE repo_id = 1 AND digest = '%s';
			UPDATE trivy_security_info SET next_check_at = 13200, checked_at = 9600 WHERE repo_id = 1 AND digest = '%s';
			UPDATE trivy_security_info SET vuln_status = 'Critical', next_check_at = 13200, checked_at = 9600 WHERE repo_id = 1 AND digest = '%s';
			UPDATE vuln_info SET next_check_at = 13200, checked_at = 9600 WHERE repo_id = 1 AND digest = '%s';
			UPDATE vuln_info SET next_check_at = 9720, checked_at = 9600 WHERE repo_id = 1 AND digest = '%s';
			UPDATE vuln_info SET status = 'Low', next_check_at = 13200, checked_at = 9600 WHERE repo_id = 1 AND digest = '%s';
		`, images[0].Manifest.Digest, imageList.Manifest.Digest, images[2].Manifest.Digest, images[1].Manifest.Digest,
			images[0].Manifest.Digest, images[2].Manifest.Digest, images[1].Manifest.Digest)
	})
}

func TestCheckVulnerabilitiesForNextManifestWithError(t *testing.T) {
	test.WithRoundTripper(func(_ *test.RoundTripper) {
		j, s := setup(t, test.WithClairDouble, test.WithTrivyDouble)
		s.Clock.StepBy(1 * time.Hour)
		tr, _ := easypg.NewTracker(t, s.DB.DbMap.Db)
		trivyJob := j.CheckTrivySecurityStatusJob(s.Registry)

		image := test.GenerateImage(test.GenerateExampleLayer(4))
		image.MustUpload(t, s, fooRepoRef, "latest")
		tr.DBChanges().Ignore()

		// submit manifest to clair
		s.ClairDouble.IndexFixtures[image.Manifest.Digest] = "fixtures/clair/manifest-004.json"
		expectSuccess(t, ExecuteOne(j.CheckVulnerabilitiesForNextManifest()))
		expectError(t, sql.ErrNoRows.Error(), ExecuteOne(j.CheckVulnerabilitiesForNextManifest()))
		tr.DBChanges().AssertEqualf(`
			UPDATE blobs SET blocks_vuln_scanning = FALSE WHERE id = 1 AND account_name = 'test1' AND digest = '%[1]s';
			UPDATE vuln_info SET next_check_at = %[3]d, checked_at = %[4]d, index_started_at = %[4]d, index_state = '%[5]s', check_duration_secs = 0 WHERE repo_id = 1 AND digest = '%[2]s';
	`, image.Layers[0].Digest, image.Manifest.Digest, s.Clock.Now().Add(2*time.Minute).Unix(), s.Clock.Now().Unix(), test.IndexStateHash)
		assert.DeepEqual(t, "delete counter", s.ClairDouble.IndexDeleteCounter, 0)

		// simulate transient error
		s.Clock.StepBy(30 * time.Minute)
		s.ClairDouble.IndexFixtures[image.Manifest.Digest] = "fixtures/clair/manifest-004.json"
		s.ClairDouble.IndexReportFixtures[image.Manifest.Digest] = "fixtures/clair/report-error.json"
		s.TrivyDouble.ReportError[image.ImageRef(s, fooRepoRef)] = true
		expectSuccess(t, ExecuteOne(j.CheckVulnerabilitiesForNextManifest()))
		expectedError := fmt.Sprintf("could not process task for job \"check trivy security status\": cannot check manifest test1/foo@%s: trivy proxy did not return 200: 500 simulated error\n", image.Manifest.Digest)
		expectError(t, expectedError, trivyJob.ProcessOne(s.Ctx))
		expectError(t, sql.ErrNoRows.Error(), ExecuteOne(j.CheckVulnerabilitiesForNextManifest()))
		tr.DBChanges().AssertEqualf(`
			UPDATE vuln_info SET next_check_at = %[2]d, checked_at = %[3]d, index_started_at = %[3]d WHERE repo_id = 1 AND digest = '%[1]s';
		`, image.Manifest.Digest, s.Clock.Now().Add(2*time.Minute).Unix(), s.Clock.Now().Unix())
		assert.DeepEqual(t, "delete counter", s.ClairDouble.IndexDeleteCounter, 1)

		// transient error fixed itself after deletion
		s.Clock.StepBy(30 * time.Minute)
		s.ClairDouble.IndexFixtures[image.Manifest.Digest] = "fixtures/clair/manifest-004.json"
		s.ClairDouble.IndexReportFixtures[image.Manifest.Digest] = ""
		s.ClairDouble.ReportFixtures[image.Manifest.Digest] = "fixtures/clair/report-vulnerable.json"
		s.TrivyDouble.ReportError[image.ImageRef(s, fooRepoRef)] = false
		s.TrivyDouble.ReportFixtures[image.ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-vulnerable.json"
		expectSuccess(t, ExecuteOne(j.CheckVulnerabilitiesForNextManifest()))
		expectSuccess(t, trivyJob.ProcessOne(s.Ctx))
		expectError(t, sql.ErrNoRows.Error(), ExecuteOne(j.CheckVulnerabilitiesForNextManifest()))
		expectError(t, sql.ErrNoRows.Error(), trivyJob.ProcessOne(s.Ctx))
		tr.DBChanges().AssertEqualf(`
			UPDATE trivy_security_info SET vuln_status = 'Critical', next_check_at = %[2]d, checked_at = %[3]d, check_duration_secs = 0 WHERE repo_id = 1 AND digest = '%[1]s';
			UPDATE vuln_info SET status = '%[4]s', next_check_at = %[2]d, checked_at = %[3]d, index_finished_at = %[3]d WHERE repo_id = 1 AND digest = '%[1]s';
		`, image.Manifest.Digest, s.Clock.Now().Add(60*time.Minute).Unix(), s.Clock.Now().Unix(), clair.LowSeverity)
		assert.DeepEqual(t, "delete counter", s.ClairDouble.IndexDeleteCounter, 1)

		// also the clair configuration was updated to make transient errors less likely to happen
		s.Clock.StepBy(10 * time.Minute)
		s.ClairDouble.IndexState = "a8b9e94aa9c8e4bb2818af1f52507b0b"
		expectSuccess(t, j.CheckClairManifestState())
		tr.DBChanges().AssertEqualf(`
			UPDATE vuln_info SET status = '%[3]s', next_check_at = %[2]d, index_state = '' WHERE repo_id = 1 AND digest = '%[1]s';
		`, image.Manifest.Digest, s.Clock.Now().Unix(), clair.PendingVulnerabilityStatus)
		assert.DeepEqual(t, "delete counter", s.ClairDouble.IndexDeleteCounter, 2)

		// clair is not done yet creating the report
		expectSuccess(t, ExecuteOne(j.CheckVulnerabilitiesForNextManifest()))
		expectError(t, sql.ErrNoRows.Error(), ExecuteOne(j.CheckVulnerabilitiesForNextManifest()))
		tr.DBChanges().AssertEqualf(`
			UPDATE vuln_info SET next_check_at = %[2]d, checked_at = %[3]d WHERE repo_id = 1 AND digest = '%[1]s';
		`, image.Manifest.Digest, s.Clock.Now().Add(2*time.Minute).Unix(), s.Clock.Now().Unix())

		// now clair is done
		s.Clock.StepBy(10 * time.Minute)
		s.ClairDouble.ReportFixtures[image.Manifest.Digest] = "fixtures/clair/report-vulnerable.json"
		expectSuccess(t, ExecuteOne(j.CheckVulnerabilitiesForNextManifest()))
		expectError(t, sql.ErrNoRows.Error(), ExecuteOne(j.CheckVulnerabilitiesForNextManifest()))
		tr.DBChanges().AssertEqualf(`
			UPDATE vuln_info SET status = '%[4]s', next_check_at = %[2]d, checked_at = %[3]d WHERE repo_id = 1 AND digest = '%[1]s';
		`, image.Manifest.Digest, s.Clock.Now().Add(60*time.Minute).Unix(), s.Clock.Now().Unix(), clair.LowSeverity)
	})
}

func TestCheckTrivySecurityStatusWithPolicies(t *testing.T) {
	test.WithRoundTripper(func(_ *test.RoundTripper) {
		j, s := setup(t, test.WithTrivyDouble)
		tr, _ := easypg.NewTracker(t, s.DB.DbMap.Db)
		trivyJob := j.CheckTrivySecurityStatusJob(s.Registry)

		//upload an example image
		image := test.GenerateImage(test.GenerateExampleLayer(4))
		image.MustUpload(t, s, fooRepoRef, "latest")
		tr.DBChanges().Ignore()
		s.TrivyDouble.ReportFixtures[image.ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-vulnerable-with-fixes.json"

		//test baseline without policies
		s.Clock.StepBy(1 * time.Hour)
		expectSuccess(t, trivyJob.ProcessOne(s.Ctx))
		expectError(t, sql.ErrNoRows.Error(), trivyJob.ProcessOne(s.Ctx))
		tr.DBChanges().AssertEqualf(`
			UPDATE blobs SET blocks_vuln_scanning = FALSE WHERE id = 1 AND account_name = 'test1' AND digest = '%[1]s';
			UPDATE trivy_security_info SET vuln_status = '%[2]s', next_check_at = %[3]d, checked_at = %[4]d, check_duration_secs = 0 WHERE repo_id = 1 AND digest = '%[5]s';
		`,
			image.Layers[0].Digest,
			clair.CriticalSeverity, s.Clock.Now().Add(60*time.Minute).Unix(), s.Clock.Now().Unix(), image.Manifest.Digest)

		//the actual checks in this test all look similar: we update the policies
		//on the account, then check the resulting vuln_status on the image
		expect := func(severity clair.VulnerabilityStatus, policies ...keppel.SecurityScanPolicy) {
			t.Helper()
			policyJSON := must.Return(json.Marshal(policies))
			mustExec(t, s.DB, `UPDATE accounts SET security_scan_policies_json = $1`, string(policyJSON))
			//ensure that `SET vuln_status = ...` always shows up in the diff below
			mustExec(t, s.DB, `UPDATE trivy_security_info SET vuln_status = $1`, clair.PendingVulnerabilityStatus)
			tr.DBChanges().Ignore()

			s.Clock.StepBy(1 * time.Hour)
			expectSuccess(t, trivyJob.ProcessOne(s.Ctx))
			expectError(t, sql.ErrNoRows.Error(), trivyJob.ProcessOne(s.Ctx))

			tr.DBChanges().AssertEqualf(`
				UPDATE trivy_security_info SET vuln_status = '%[1]s', next_check_at = %[2]d, checked_at = %[3]d WHERE repo_id = 1 AND digest = '%[4]s';
			`, severity, s.Clock.Now().Add(60*time.Minute).Unix(), s.Clock.Now().Unix(), image.Manifest.Digest)
		}

		//set a policy that downgrades the one "Critical" vuln -> this downgrades
		//the overall status to "High" since there are also several "High" vulns
		//
		//Most of the following testcases are alterations of this policy.
		expect(clair.HighSeverity, keppel.SecurityScanPolicy{
			RepositoryRx:      ".*",
			VulnerabilityIDRx: "CVE-2019-8457",
			Action: keppel.SecurityScanPolicyAction{
				Assessment: "we accept the risk",
				Severity:   clair.LowSeverity,
			},
		})

		//test Action.Ignore -> same result
		expect(clair.HighSeverity, keppel.SecurityScanPolicy{
			RepositoryRx:      ".*",
			VulnerabilityIDRx: "CVE-2019-8457",
			Action: keppel.SecurityScanPolicyAction{
				Assessment: "we accept the risk",
				Ignore:     true,
			},
		})

		//test RepositoryRx
		expect(clair.CriticalSeverity, keppel.SecurityScanPolicy{
			RepositoryRx:      "bar", //does not match our test repo
			VulnerabilityIDRx: "CVE-2019-8457",
			Action: keppel.SecurityScanPolicyAction{
				Assessment: "we accept the risk",
				Severity:   clair.LowSeverity,
			},
		})

		//test NegativeRepositoryRx
		expect(clair.CriticalSeverity, keppel.SecurityScanPolicy{
			RepositoryRx:         ".*",
			NegativeRepositoryRx: "foo", //matches our test repo
			VulnerabilityIDRx:    "CVE-2019-8457",
			Action: keppel.SecurityScanPolicyAction{
				Assessment: "we accept the risk",
				Severity:   clair.LowSeverity,
			},
		})

		//test NegativeVulnerabilityIDRx
		expect(clair.CriticalSeverity, keppel.SecurityScanPolicy{
			RepositoryRx:              ".*",
			VulnerabilityIDRx:         ".*",
			NegativeVulnerabilityIDRx: "CVE-2019-8457",
			Action: keppel.SecurityScanPolicyAction{
				Assessment: "we accept the risk",
				Severity:   clair.LowSeverity,
			},
		})

		//test ExceptFixReleased on its own (the highest vulnerability with a
		//released fix is "High")
		expect(clair.HighSeverity, keppel.SecurityScanPolicy{
			RepositoryRx:      ".*",
			VulnerabilityIDRx: ".*",
			ExceptFixReleased: true,
			Action: keppel.SecurityScanPolicyAction{
				Assessment: "we can only update if a fix is available",
				Ignore:     true,
			},
		})

		//test ExceptFixReleased together with an ignore of all high-severity fixed
		//vulns (the next highest vulnerability with a released fix is "Medium")
		expect(clair.MediumSeverity,
			keppel.SecurityScanPolicy{
				RepositoryRx:      ".*",
				VulnerabilityIDRx: ".*",
				ExceptFixReleased: true,
				Action: keppel.SecurityScanPolicyAction{
					Assessment: "we can only update if a fix is available",
					Ignore:     true,
				},
			},
			keppel.SecurityScanPolicy{
				RepositoryRx:      ".*",
				VulnerabilityIDRx: "CVE-2022-29458", //matches vulnerabilities in multiple packages
				Action: keppel.SecurityScanPolicyAction{
					Assessment: "will fix tomorrow, I swear",
					Severity:   clair.LowSeverity,
				},
			},
		)
	})
}
