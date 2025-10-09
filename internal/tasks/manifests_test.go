// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	. "github.com/majewsky/gg/option"
	"github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

////////////////////////////////////////////////////////////////////////////////
// tests for ManifestValidationJob

// Base behavior for various unit tests that start with the same image list, destroy
// it in various ways, and check that ManifestValidationJob correctly fixes it.
func testManifestValidationJobFixesDisturbance(t *testing.T, disturb func(*keppel.DB, []int64, []string)) {
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)
	validateManifestJob := j.ManifestValidationJob(s.Registry)

	var (
		allBlobIDs         []int64
		allManifestDigests []string
	)

	// setup two image manifests, both with some layers
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

	// also setup an image list manifest containing those images (so that we have
	// some manifest-manifest refs to play with)
	imageList := test.GenerateImageList(images[0], images[1])
	imageList.MustUpload(t, s, fooRepoRef, "")
	allManifestDigests = append(allManifestDigests, imageList.Manifest.Digest.String())

	// since these manifests were just uploaded, next_validation_at is set in the future,
	// so ManifestValidationJob will report that there is nothing to do
	assert.ErrEqual(t, validateManifestJob.ProcessOne(s.Ctx), sql.ErrNoRows)

	// once they need validating, they validate successfully
	s.Clock.StepBy(36 * time.Hour)
	assert.ErrEqual(t, validateManifestJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, validateManifestJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, validateManifestJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, validateManifestJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/manifest-validate-001-before-disturbance.sql")

	// disturb the DB state, then rerun ManifestValidationJob to fix it
	s.Clock.StepBy(36 * time.Hour)
	disturb(s.DB, allBlobIDs, allManifestDigests)
	assert.ErrEqual(t, validateManifestJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, validateManifestJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, validateManifestJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, validateManifestJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/manifest-validate-002-after-fix.sql")
}

func TestManifestValidationJobFixesWrongSize(t *testing.T) {
	testManifestValidationJobFixesDisturbance(t, func(db *keppel.DB, allBlobIDs []int64, allManifestDigests []string) {
		_, _ = allBlobIDs, allManifestDigests
		test.MustExec(t, db, `UPDATE manifests SET size_bytes = 1337`)
	})
}

func TestManifestValidationJobFixesMissingManifestBlobRefs(t *testing.T) {
	testManifestValidationJobFixesDisturbance(t, func(db *keppel.DB, allBlobIDs []int64, allManifestDigests []string) {
		_, _ = allBlobIDs, allManifestDigests
		test.MustExec(t, db, `DELETE FROM manifest_blob_refs WHERE blob_id % 2 = 0`)
	})
}

func TestManifestValidationJobFixesMissingManifestManifestRefs(t *testing.T) {
	testManifestValidationJobFixesDisturbance(t, func(db *keppel.DB, allBlobIDs []int64, allManifestDigests []string) {
		_, _ = allBlobIDs, allManifestDigests
		test.MustExec(t, db, `DELETE FROM manifest_manifest_refs`)
	})
}

func TestManifestValidationJobFixesSuperfluousManifestBlobRefs(t *testing.T) {
	testManifestValidationJobFixesDisturbance(t, func(db *keppel.DB, allBlobIDs []int64, allManifestDigests []string) {
		for _, id := range allBlobIDs {
			for _, d := range allManifestDigests {
				test.MustExec(t, db, `INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, $1, $2) ON CONFLICT DO NOTHING`, d, id)
			}
		}
	})
}

func TestManifestValidationJobFixesSuperfluousManifestManifestRefs(t *testing.T) {
	testManifestValidationJobFixesDisturbance(t, func(db *keppel.DB, allBlobIDs []int64, allManifestDigests []string) {
		_ = allBlobIDs
		for _, d1 := range allManifestDigests {
			for _, d2 := range allManifestDigests {
				test.MustExec(t, db, `INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, $1, $2) ON CONFLICT DO NOTHING`, d1, d2)
			}
		}
	})
}

func TestManifestValidationJobError(t *testing.T) {
	j, s := setup(t)
	validateManifestJob := j.ManifestValidationJob(s.Registry)

	// setup a manifest that is missing a referenced blob (we need to do this
	// manually since the MustUpload functions care about uploading stuff intact)
	s.Clock.StepBy(1 * time.Hour)
	image := test.GenerateImage( /* no layers */ )
	must.SucceedT(t, s.DB.Insert(&models.Manifest{
		RepositoryID:     1,
		Digest:           image.Manifest.Digest,
		MediaType:        image.Manifest.MediaType,
		SizeBytes:        image.SizeBytes(),
		PushedAt:         s.Clock.Now(),
		NextValidationAt: s.Clock.Now().Add(models.ManifestValidationInterval),
	}))
	must.SucceedT(t, s.DB.Insert(&models.ManifestContent{
		RepositoryID: 1,
		Digest:       image.Manifest.Digest.String(),
		Content:      image.Manifest.Contents,
	}))
	must.SucceedT(t, s.DB.Insert(&models.TrivySecurityInfo{
		RepositoryID:        1,
		Digest:              image.Manifest.Digest,
		NextCheckAt:         Some(time.Unix(0, 0)),
		VulnerabilityStatus: models.PendingVulnerabilityStatus,
	}))
	must.SucceedT(t, s.SD.WriteManifest(s.Ctx, s.Accounts[0].Reduced(), "foo", image.Manifest.Digest, image.Manifest.Contents))

	// validation should yield an error
	s.Clock.StepBy(36 * time.Hour)
	expectedError := fmt.Sprintf(
		"while validating manifest %s in repo 1: manifest blob unknown to registry: %s",
		image.Manifest.Digest, image.Config.Digest,
	)
	assert.ErrEqual(t, validateManifestJob.ProcessOne(s.Ctx), expectedError)

	// check that validation error to be recorded in the DB
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/manifest-validate-error-001.sql")

	// expect next ManifestValidationJob run to skip over this manifest since it
	// was recently validated
	assert.ErrEqual(t, validateManifestJob.ProcessOne(s.Ctx), sql.ErrNoRows)

	// upload missing blob so that we can test recovering from the validation error
	image.Config.MustUpload(t, s, fooRepoRef)

	// next validation should be happy (and also create the missing refs)
	s.Clock.StepBy(36 * time.Hour)
	assert.ErrEqual(t, validateManifestJob.ProcessOne(s.Ctx), nil)
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/manifest-validate-error-002.sql")
}

////////////////////////////////////////////////////////////////////////////////
// tests for ManifestSyncJob

func TestManifestSyncJob(t *testing.T) {
	forAllReplicaTypes(t, func(strategy string) {
		test.WithRoundTripper(func(_ *test.RoundTripper) {
			j1, s1 := setup(t)
			j2, s2 := setupReplica(t, s1, strategy)
			s1.Clock.StepBy(1 * time.Hour)
			replicaToken := s2.GetToken(t, "repository:test1/foo:pull")
			syncManifestsJob1 := j1.ManifestSyncJob(s1.Registry)
			syncManifestsJob2 := j2.ManifestSyncJob(s2.Registry)

			// upload some manifests...
			images := make([]test.Image, 4)
			for idx := range images {
				image := test.GenerateImage(
					test.GenerateExampleLayer(int64(10*idx+1)),
					test.GenerateExampleLayer(int64(10*idx+2)),
				)
				images[idx] = image

				// ...to the primary account...
				image.MustUpload(t, s1, fooRepoRef, "")

				// ...and most of them also to the replica account (to simulate replication having taken place)
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

			// some of the replicated images are also tagged
			for _, db := range []*keppel.DB{s1.DB, s2.DB} {
				for _, tagName := range []string{"latest", "other"} {
					test.MustExec(t, db,
						`INSERT INTO tags (repo_id, name, digest, pushed_at) VALUES (1, $1, $2, $3)`,
						tagName,
						images[1].Manifest.Digest,
						s1.Clock.Now(),
					)
				}
			}

			// also setup an image list manifest containing some of those images (so that we have
			// some manifest-manifest refs to play with)
			imageList := test.GenerateImageList(images[1], images[2])
			imageList.MustUpload(t, s1, fooRepoRef, "")
			// this one is replicated as well
			assert.HTTPRequest{
				Method:       "GET",
				Path:         fmt.Sprintf("/v2/test1/foo/manifests/%s", imageList.Manifest.Digest),
				Header:       map[string]string{"Authorization": "Bearer " + replicaToken},
				ExpectStatus: http.StatusOK,
				ExpectBody:   assert.ByteData(imageList.Manifest.Contents),
			}.Check(t, s2.Handler)

			// set a well-known last_pulled_at timestamp on all manifests in the primary
			// DB (we will later verify that this was not touched by the manifest sync)
			initialLastPulledAt := time.Unix(42, 0)
			test.MustExec(t, s1.DB, `UPDATE manifests SET last_pulled_at = $1`, initialLastPulledAt)
			test.MustExec(t, s1.DB, `UPDATE tags SET last_pulled_at = $1`, initialLastPulledAt)
			// we set last_pulled_at to NULL on images[3] to verify that we can merge
			// NULL with a non-NULL last_pulled_at from the replica side
			test.MustExec(t, s1.DB, `UPDATE manifests SET last_pulled_at = NULL WHERE digest = $1`, images[3].Manifest.Digest)

			// as an exception, in the on_first_use method, we can and want to merge
			// last_pulled_at timestamps from the replica into those of the primary, so
			// set some of those to verify the merging behavior
			earlierLastPulledAt := initialLastPulledAt.Add(-10 * time.Second)
			laterLastPulledAt := initialLastPulledAt.Add(+10 * time.Second)
			test.MustExec(t, s2.DB, `UPDATE manifests SET last_pulled_at = NULL`)
			test.MustExec(t, s2.DB, `UPDATE tags SET last_pulled_at = NULL`)
			test.MustExec(t, s2.DB, `UPDATE manifests SET last_pulled_at = $1 WHERE digest = $2`, earlierLastPulledAt, images[1].Manifest.Digest)
			test.MustExec(t, s2.DB, `UPDATE manifests SET last_pulled_at = $1 WHERE digest = $2`, laterLastPulledAt, images[2].Manifest.Digest)
			test.MustExec(t, s2.DB, `UPDATE manifests SET last_pulled_at = $1 WHERE digest = $2`, initialLastPulledAt, images[3].Manifest.Digest)
			test.MustExec(t, s2.DB, `UPDATE tags SET last_pulled_at = $1 WHERE name = $2`, earlierLastPulledAt, "latest")
			test.MustExec(t, s2.DB, `UPDATE tags SET last_pulled_at = $1 WHERE name = $2`, laterLastPulledAt, "other")

			tr, tr0 := easypg.NewTracker(t, s2.DB.Db)
			tr0.AssertEqualToFile(fmt.Sprintf("fixtures/manifest-sync-setup-%s.sql", strategy))
			trForPrimary, _ := easypg.NewTracker(t, s1.DB.Db)

			// ManifestSyncJob on the primary registry should have nothing to do
			// since there are no replica accounts
			assert.ErrEqual(t, syncManifestsJob1.ProcessOne(s1.Ctx), sql.ErrNoRows)
			trForPrimary.DBChanges().AssertEmpty()
			// ManifestSyncJob on the secondary registry should set the
			// ManifestsSyncedAt timestamp on the repo, but otherwise not do anything
			assert.ErrEqual(t, syncManifestsJob2.ProcessOne(s2.Ctx), nil)
			tr.DBChanges().AssertEqualf(`
					UPDATE repos SET next_manifest_sync_at = %d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
				`,
				s1.Clock.Now().Add(1*time.Hour).Unix(),
			)
			// second run should not have anything else to do
			assert.ErrEqual(t, syncManifestsJob2.ProcessOne(s2.Ctx), sql.ErrNoRows)
			tr.DBChanges().AssertEmpty()

			// in on_first_use, the sync should have merged the replica's last_pulled_at
			// timestamps into the primary, i.e. primary.last_pulled_at =
			// max(primary.last_pulled_at, replica.last_pulled_at); this only touches
			// the DB when the replica's last_pulled_at is after the primary's
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
				// reset all timestamps to prevent divergences in the rest of the test
				test.MustExec(t, s1.DB, `UPDATE manifests SET last_pulled_at = $1`, initialLastPulledAt)
				test.MustExec(t, s1.DB, `UPDATE tags SET last_pulled_at = $1`, initialLastPulledAt)
				test.MustExec(t, s2.DB, `UPDATE manifests SET last_pulled_at = $1`, initialLastPulledAt)
				test.MustExec(t, s2.DB, `UPDATE tags SET last_pulled_at = $1`, initialLastPulledAt)
				tr.DBChanges() // skip these changes
			} else {
				trForPrimary.DBChanges().AssertEmpty()
			}

			// delete a manifest on the primary side (this one is a simple image not referenced by anyone else)
			s1.Clock.StepBy(2 * time.Hour)
			test.MustExec(t, s1.DB,
				`DELETE FROM manifests WHERE digest = $1`,
				images[3].Manifest.Digest,
			)
			// move a tag on the primary side
			test.MustExec(t, s1.DB,
				`UPDATE tags SET digest = $1 WHERE name = 'latest'`,
				images[2].Manifest.Digest,
			)

			// again, nothing to do on the primary side
			assert.ErrEqual(t, syncManifestsJob1.ProcessOne(s1.Ctx), sql.ErrNoRows)
			// ManifestSyncJob on the replica side should not do anything while
			// the account is in maintenance; only the timestamp is updated to make sure
			// that the job loop progresses to the next repo
			test.MustExec(t, s2.DB, `UPDATE accounts SET is_deleting = TRUE`)
			assert.ErrEqual(t, syncManifestsJob2.ProcessOne(s2.Ctx), nil)
			tr.DBChanges().AssertEqualf(`
					UPDATE accounts SET is_deleting = TRUE WHERE name = 'test1';
					UPDATE repos SET next_manifest_sync_at = %d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
				`,
				s1.Clock.Now().Add(1*time.Hour).Unix(),
			)
			assert.ErrEqual(t, syncManifestsJob2.ProcessOne(s2.Ctx), sql.ErrNoRows)
			tr.DBChanges().AssertEmpty()

			// end deletion
			test.MustExec(t, s2.DB, `UPDATE accounts SET is_deleting = FALSE`)
			tr.DBChanges().AssertEqual(`UPDATE accounts SET is_deleting = FALSE WHERE name = 'test1';`)

			// test that replication from external uses the inbound cache
			if strategy == "from_external_on_first_use" {
				// after the end of the maintenance, we would naively expect
				// ManifestSyncJob to actually replicate the deletion, BUT we have an
				// inbound cache with a lifetime of 6 hours, so actually nothing should
				// happen (only the tag gets synced, which includes a validation of the
				// referenced manifest)
				s1.Clock.StepBy(2 * time.Hour)
				assert.ErrEqual(t, syncManifestsJob2.ProcessOne(s2.Ctx), nil)
				tr.DBChanges().AssertEqualf(`
						UPDATE manifests SET next_validation_at = %d WHERE repo_id = 1 AND digest = '%s';
						UPDATE repos SET next_manifest_sync_at = %d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
					`,
					s1.Clock.Now().Add(models.ManifestValidationInterval).Unix(),
					images[1].Manifest.Digest,
					s1.Clock.Now().Add(1*time.Hour).Unix(),
				)
				assert.ErrEqual(t, syncManifestsJob2.ProcessOne(s2.Ctx), sql.ErrNoRows)
				tr.DBChanges().AssertEmpty()
			}

			// From now on, we will go in clock increments of 7 hours to force the
			// inbound cache to never hit.

			// after the end of the maintenance, ManifestSyncJob on the replica
			// side should delete the same manifest that we deleted in the primary
			// account, and also replicate the tag change (which includes a validation
			// of the tagged manifests)
			s1.Clock.StepBy(7 * time.Hour)
			assert.ErrEqual(t, syncManifestsJob2.ProcessOne(s2.Ctx), nil)
			manifestValidationBecauseOfExistingTag := fmt.Sprintf(
				// this validation is skipped in "on_first_use" because the respective tag is unchanged
				`UPDATE manifests SET next_validation_at = %d WHERE repo_id = 1 AND digest = '%s';`+"\n",
				s1.Clock.Now().Add(models.ManifestValidationInterval).Unix(), images[1].Manifest.Digest,
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
					%[5]sUPDATE manifests SET next_validation_at = %[6]d WHERE repo_id = 1 AND digest = '%[3]s';
					UPDATE repos SET next_manifest_sync_at = %[4]d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
					UPDATE tags SET digest = '%[3]s', pushed_at = %[2]d, last_pulled_at = NULL WHERE repo_id = 1 AND name = 'latest';
					DELETE FROM trivy_security_info WHERE repo_id = 1 AND digest = '%[1]s';
				`,
				images[3].Manifest.Digest, // the deleted manifest
				s1.Clock.Now().Unix(),
				images[2].Manifest.Digest, // the manifest now tagged as "latest"
				s1.Clock.Now().Add(1*time.Hour).Unix(),
				manifestValidationBecauseOfExistingTag,
				s1.Clock.Now().Add(models.ManifestValidationInterval).Unix(),
			)
			assert.ErrEqual(t, syncManifestsJob2.ProcessOne(s2.Ctx), sql.ErrNoRows)
			tr.DBChanges().AssertEmpty()

			// cause a deliberate inconsistency on the primary side: delete a manifest that
			// *is* referenced by another manifest (this requires deleting the
			// manifest-manifest ref first, otherwise the DB will complain)
			s1.Clock.StepBy(7 * time.Hour)
			test.MustExec(t, s1.DB,
				`DELETE FROM manifest_manifest_refs WHERE child_digest = $1`,
				images[2].Manifest.Digest,
			)
			test.MustExec(t, s1.DB,
				`DELETE FROM manifests WHERE digest = $1`,
				images[2].Manifest.Digest,
			)

			// ManifestSyncJob should now complain since it wants to delete
			// images[2].Manifest, but it can't because of the manifest-manifest ref to
			// the image list
			expectedError := fmt.Sprintf("while syncing manifests in repo test1/foo: cannot remove deleted manifests [%s] because they are still being referenced by other manifests (this smells like an inconsistency on the primary account)",
				images[2].Manifest.Digest,
			)
			assert.ErrEqual(t, syncManifestsJob2.ProcessOne(s2.Ctx), expectedError)
			// the tag sync went through though, so the tag should be gone (the manifest
			// validation is because of the "other" tag that still exists)
			manifestValidationBecauseOfExistingTag = fmt.Sprintf(
				// this validation is skipped in "on_first_use" because the respective tag is unchanged
				`UPDATE manifests SET next_validation_at = %d WHERE repo_id = 1 AND digest = '%s';`+"\n",
				s1.Clock.Now().Add(models.ManifestValidationInterval).Unix(), images[1].Manifest.Digest,
			)
			if strategy == "on_first_use" {
				manifestValidationBecauseOfExistingTag = ""
			}
			tr.DBChanges().AssertEqualf(`%sDELETE FROM tags WHERE repo_id = 1 AND name = 'latest';`,
				manifestValidationBecauseOfExistingTag,
			)

			// also remove the image list manifest on the primary side
			s1.Clock.StepBy(7 * time.Hour)
			test.MustExec(t, s1.DB,
				`DELETE FROM manifests WHERE digest = $1`,
				imageList.Manifest.Digest,
			)
			// and remove the other tag (this is required for the 404 error message in the next step but one to be deterministic)
			test.MustExec(t, s1.DB, `DELETE FROM tags`)

			// this makes the primary side consistent again, so ManifestSyncJob
			// should succeed now and remove both deleted manifests from the DB
			assert.ErrEqual(t, syncManifestsJob2.ProcessOne(s2.Ctx), nil)
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
				`,
				images[2].Manifest.Digest,
				imageList.Manifest.Digest,
				images[1].Manifest.Digest,
				s1.Clock.Now().Add(1*time.Hour).Unix(),
			)
			assert.ErrEqual(t, syncManifestsJob2.ProcessOne(s2.Ctx), sql.ErrNoRows)
			tr.DBChanges().AssertEmpty()

			// replace the primary registry's API with something that just answers 404 most of the time
			//
			// (We do allow the /keppel/v1/auth endpoint to work properly because
			// otherwise the error messages are not reproducible between passes.)
			s1.Clock.StepBy(7 * time.Hour)
			http.DefaultTransport.(*test.RoundTripper).Handlers["registry.example.org"] = answerMostWith404(s1.Handler)
			// This is particularly devious since 404 is returned by the GET endpoint for
			// a manifest when the manifest was deleted. We want to check that the next
			// ManifestSyncJob understands that this is a network issue and not
			// caused by the manifest getting deleted, since the 404-generating endpoint
			// does not render a proper MANIFEST_UNKNOWN error.
			expectedError = fmt.Sprintf("while syncing manifests in repo test1/foo: cannot check existence of manifest %s on primary account: during GET https://registry.example.org/v2/test1/foo/manifests/%[1]s: expected status 200, but got 404 Not Found",
				images[1].Manifest.Digest, // the only manifest that is left
			)
			assert.ErrEqual(t, syncManifestsJob2.ProcessOne(s2.Ctx), expectedError)
			tr.DBChanges().AssertEmpty()

			// check that the manifest sync did not update the last_pulled_at timestamps
			// in the primary DB (even though there were GET requests for the manifests
			// there)
			var lastPulledAt time.Time
			must.SucceedT(t, s1.DB.DbMap.QueryRow(`SELECT MAX(last_pulled_at) FROM manifests`).Scan(&lastPulledAt))
			if !lastPulledAt.Equal(initialLastPulledAt) {
				t.Error("last_pulled_at timestamps on the primary side were touched")
				t.Logf("  expected = %#v", initialLastPulledAt)
				t.Logf("  actual   = %#v", lastPulledAt)
			}

			// flip back to the actual primary registry's API
			http.DefaultTransport.(*test.RoundTripper).Handlers["registry.example.org"] = s1.Handler
			// delete the entire repository on the primary
			s1.Clock.StepBy(7 * time.Hour)
			test.MustExec(t, s1.DB, `DELETE FROM manifests`)
			test.MustExec(t, s1.DB, `DELETE FROM repos`)
			// the manifest sync should reflect the repository deletion on the replica
			assert.ErrEqual(t, syncManifestsJob2.ProcessOne(s2.Ctx), nil)
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
				`,
				images[1].Manifest.Digest,
			)
			assert.ErrEqual(t, syncManifestsJob2.ProcessOne(s2.Ctx), sql.ErrNoRows)
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
// tests for CheckTrivySecurityStatusJob

func TestCheckTrivySecurityStatus(t *testing.T) {
	test.WithRoundTripper(func(_ *test.RoundTripper) {
		j, s := setup(t, test.WithTrivyDouble)
		s.Clock.StepBy(1 * time.Hour)

		// setup two image manifests with just one content layer (we don't really care about
		// the content since our Trivy double doesn't care either)
		images := make([]test.Image, 3, 4)
		imageManifests := make([]models.Manifest, 3, 4)
		for idx := range images {
			images[idx] = test.GenerateImage(test.GenerateExampleLayer(int64(idx)))
			imageManifests[idx] = images[idx].MustUpload(t, s, fooRepoRef, "")
		}
		// generate a 2 MiB big image to run into blobUncompressedSizeTooBigGiB
		bigImage := test.GenerateImage(test.GenerateExampleLayerSize(int64(2), 2))
		images = append(images, bigImage)
		imageManifests = append(imageManifests, bigImage.MustUpload(t, s, fooRepoRef, ""))

		// also setup an image list manifest containing those images (so that we have
		// some manifest-manifest refs to play with)
		imageList := test.GenerateImageList(images[0], images[1])
		imageListManifest := imageList.MustUpload(t, s, fooRepoRef, "")

		// adjust too big values down to make testing easier
		blobUncompressedSizeTooBigGiB = 0.001

		tr, tr0 := easypg.NewTracker(t, s.DB.Db)
		tr0.AssertEqualToFile("fixtures/vulnerability-check-setup.sql")

		// before the security check, there are no reports stored in storage
		for _, manifest := range imageManifests {
			s.ExpectTrivyReportMissingInStorage(t, manifest, "json")
		}
		s.ExpectTrivyReportMissingInStorage(t, imageListManifest, "json")

		trivyJob := j.CheckTrivySecurityStatusJob(s.Registry)

		// check that security check updates vulnerability status
		s.TrivyDouble.ReportFixtures[images[0].ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-vulnerable.json"
		s.TrivyDouble.ReportFixtures[images[1].ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-clean.json"
		s.TrivyDouble.ReportFixtures[images[2].ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-vulnerable.json"
		s.TrivyDouble.ReportFixtures[images[3].ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-clean.json"
		s.Clock.StepBy(5 * time.Minute)
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), nil)
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), sql.ErrNoRows)
		tr.DBChanges().AssertEqualf(`
			UPDATE blobs SET blocks_vuln_scanning = FALSE WHERE id = 1 AND account_name = 'test1' AND digest = '%[10]s';
			UPDATE blobs SET blocks_vuln_scanning = FALSE WHERE id = 3 AND account_name = 'test1' AND digest = '%[11]s';
			UPDATE blobs SET blocks_vuln_scanning = FALSE WHERE id = 5 AND account_name = 'test1' AND digest = '%[12]s';
			UPDATE blobs SET blocks_vuln_scanning = TRUE WHERE id = 7 AND account_name = 'test1' AND digest = '%[13]s';
			UPDATE trivy_security_info SET vuln_status = 'Critical', next_check_at = %[7]d, checked_at = %[6]d, check_duration_secs = 0, has_enriched_report = TRUE WHERE repo_id = 1 AND digest = '%[1]s';
			UPDATE trivy_security_info SET next_check_at = %[7]d, checked_at = %[6]d, check_duration_secs = 0 WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE trivy_security_info SET vuln_status = 'Critical', next_check_at = %[7]d, checked_at = %[6]d, check_duration_secs = 0, has_enriched_report = TRUE WHERE repo_id = 1 AND digest = '%[3]s';
			UPDATE trivy_security_info SET vuln_status = 'Unsupported', message = 'vulnerability scanning is not supported for uncompressed image layers above %[9]g GiB', next_check_at = %[8]d WHERE repo_id = 1 AND digest = '%[4]s';
			UPDATE trivy_security_info SET vuln_status = 'Clean', next_check_at = %[7]d, checked_at = %[6]d, check_duration_secs = 0, has_enriched_report = TRUE WHERE repo_id = 1 AND digest = '%[5]s';
		`, images[0].Manifest.Digest, imageList.Manifest.Digest, images[2].Manifest.Digest, images[3].Manifest.Digest, images[1].Manifest.Digest,
			s.Clock.Now().Unix(), s.Clock.Now().Add(60*time.Minute).Unix(), s.Clock.Now().Add(24*time.Hour).Unix(), blobUncompressedSizeTooBigGiB,
			images[0].Layers[0].Digest, images[1].Layers[0].Digest, images[2].Layers[0].Digest, images[3].Layers[0].Digest)

		// for scannable images, a report should now be cached in storage
		s.ExpectTrivyReportExistsInStorage(t, imageManifests[0], "json", assert.JSONFixtureFile("fixtures/trivy/report-vulnerable.json"))
		s.ExpectTrivyReportExistsInStorage(t, imageManifests[1], "json", assert.JSONFixtureFile("fixtures/trivy/report-clean.json"))
		s.ExpectTrivyReportExistsInStorage(t, imageManifests[2], "json", assert.JSONFixtureFile("fixtures/trivy/report-vulnerable.json"))
		s.ExpectTrivyReportMissingInStorage(t, imageManifests[3], "json")
		s.ExpectTrivyReportMissingInStorage(t, imageListManifest, "json")

		// check that a changed vulnerability status does not have any unexpected side effects
		s.TrivyDouble.ReportFixtures[images[1].ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-vulnerable.json"
		s.Clock.StepBy(1 * time.Hour)
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), nil)
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), sql.ErrNoRows)
		tr.DBChanges().AssertEqualf(`
			UPDATE trivy_security_info SET next_check_at = %[6]d, checked_at = %[5]d WHERE repo_id = 1 AND digest = '%[1]s';
			UPDATE trivy_security_info SET vuln_status = 'Critical', next_check_at = %[6]d, checked_at = %[5]d WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE trivy_security_info SET next_check_at = %[6]d, checked_at = %[5]d WHERE repo_id = 1 AND digest = '%[3]s';
			UPDATE trivy_security_info SET vuln_status = 'Critical', next_check_at = %[6]d, checked_at = %[5]d, vuln_status_changed_at = %[5]d WHERE repo_id = 1 AND digest = '%[4]s';
		`, images[0].Manifest.Digest, imageList.Manifest.Digest, images[2].Manifest.Digest, images[1].Manifest.Digest,
			s.Clock.Now().Unix(), s.Clock.Now().Add(1*time.Hour).Unix(),
		)

		// check that no change in vulnerability status does not have any unexpected side effects
		s.Clock.StepBy(1 * time.Hour)
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), nil)
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), sql.ErrNoRows)
		tr.DBChanges().AssertEqualf(`
			UPDATE trivy_security_info SET next_check_at = %[6]d, checked_at = %[5]d WHERE repo_id = 1 AND digest = '%[1]s';
			UPDATE trivy_security_info SET next_check_at = %[6]d, checked_at = %[5]d WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE trivy_security_info SET next_check_at = %[6]d, checked_at = %[5]d WHERE repo_id = 1 AND digest = '%[3]s';
			UPDATE trivy_security_info SET next_check_at = %[6]d, checked_at = %[5]d WHERE repo_id = 1 AND digest = '%[4]s';
		`, images[0].Manifest.Digest, imageList.Manifest.Digest, images[2].Manifest.Digest, images[1].Manifest.Digest,
			s.Clock.Now().Unix(), s.Clock.Now().Add(1*time.Hour).Unix(),
		)

		// the changed vulnerability report was reflected in the cache
		s.ExpectTrivyReportExistsInStorage(t, imageManifests[1], "json", assert.JSONFixtureFile("fixtures/trivy/report-vulnerable.json"))
	})
}

func TestCheckTrivySecurityStatusWithError(t *testing.T) {
	test.WithRoundTripper(func(_ *test.RoundTripper) {
		j, s := setup(t, test.WithTrivyDouble)
		s.Clock.StepBy(1 * time.Hour)
		tr, _ := easypg.NewTracker(t, s.DB.Db)
		trivyJob := j.CheckTrivySecurityStatusJob(s.Registry)

		image := test.GenerateImage(test.GenerateExampleLayer(4))
		imageManifest := image.MustUpload(t, s, fooRepoRef, "latest")
		tr.DBChanges().Ignore()

		// simulate transient error
		s.Clock.StepBy(30 * time.Minute)
		s.TrivyDouble.ReportError[image.ImageRef(s, fooRepoRef)] = true
		expectedError := fmt.Sprintf("cannot check manifest test1/foo@%s: scan error: trivy proxy did not return 200: 500 simulated error", image.Manifest.Digest)
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), expectedError)
		tr.DBChanges().AssertEqualf(`
			UPDATE blobs SET blocks_vuln_scanning = FALSE WHERE id = 1 AND account_name = 'test1' AND digest = '%[1]s';
			UPDATE trivy_security_info SET vuln_status = 'Error', message = 'scan error: trivy proxy did not return 200: 500 simulated error', next_check_at = 5700 WHERE repo_id = 1 AND digest = '%[2]s';
		`, image.Layers[0].Digest, image.Manifest.Digest)

		// on error, no report gets cached
		s.ExpectTrivyReportMissingInStorage(t, imageManifest, "json")

		// transient error fixed itself after deletion
		s.Clock.StepBy(30 * time.Minute)
		s.TrivyDouble.ReportError[image.ImageRef(s, fooRepoRef)] = false
		s.TrivyDouble.ReportFixtures[image.ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-vulnerable.json"
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), nil)
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), sql.ErrNoRows)
		tr.DBChanges().AssertEqualf(`
			UPDATE trivy_security_info SET vuln_status = 'Critical', message = '', next_check_at = %[2]d, checked_at = %[3]d, check_duration_secs = 0, has_enriched_report = TRUE WHERE repo_id = 1 AND digest = '%[1]s';
		`, image.Manifest.Digest, s.Clock.Now().Add(60*time.Minute).Unix(), s.Clock.Now().Unix(), models.LowSeverity)

		// after successful scan, a report gets cached
		s.ExpectTrivyReportExistsInStorage(t, imageManifest, "json", assert.JSONFixtureFile("fixtures/trivy/report-vulnerable.json"))
	})
}

func TestCheckTrivySecurityStatusWithPolicies(t *testing.T) {
	test.WithRoundTripper(func(_ *test.RoundTripper) {
		j, s := setup(t, test.WithTrivyDouble)
		tr, _ := easypg.NewTracker(t, s.DB.Db)
		trivyJob := j.CheckTrivySecurityStatusJob(s.Registry)

		// upload an example image
		image := test.GenerateImage(test.GenerateExampleLayer(4))
		image.MustUpload(t, s, fooRepoRef, "latest")
		tr.DBChanges().Ignore()
		s.TrivyDouble.ReportFixtures[image.ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-vulnerable-with-fixes.json"

		// test baseline without policies
		s.Clock.StepBy(1 * time.Hour)
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), nil)
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), sql.ErrNoRows)
		tr.DBChanges().AssertEqualf(`
			UPDATE blobs SET blocks_vuln_scanning = FALSE WHERE id = 1 AND account_name = 'test1' AND digest = '%[1]s';
			UPDATE trivy_security_info SET vuln_status = '%[2]s', next_check_at = %[3]d, checked_at = %[4]d, check_duration_secs = 0, has_enriched_report = TRUE WHERE repo_id = 1 AND digest = '%[5]s';
		`, image.Layers[0].Digest, models.CriticalSeverity, s.Clock.Now().Add(60*time.Minute).Unix(), s.Clock.Now().Unix(), image.Manifest.Digest)

		// the actual checks in this test all look similar: we update the policies
		// on the account, then check the resulting vuln_status on the image
		expect := func(severity models.VulnerabilityStatus, policies ...keppel.SecurityScanPolicy) {
			t.Helper()
			policyJSON := must.Return(json.Marshal(policies))
			test.MustExec(t, s.DB, `UPDATE accounts SET security_scan_policies_json = $1`, string(policyJSON))
			// ensure that `SET vuln_status = ...` always shows up in the diff below
			test.MustExec(t, s.DB, `UPDATE trivy_security_info SET vuln_status = $1`, models.PendingVulnerabilityStatus)
			tr.DBChanges().Ignore()

			s.Clock.StepBy(1 * time.Hour)
			assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), nil)
			assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), sql.ErrNoRows)

			tr.DBChanges().AssertEqualf(`
				UPDATE trivy_security_info SET vuln_status = '%[1]s', next_check_at = %[2]d, checked_at = %[3]d WHERE repo_id = 1 AND digest = '%[4]s';
			`, severity, s.Clock.Now().Add(60*time.Minute).Unix(), s.Clock.Now().Unix(), image.Manifest.Digest)
		}

		// set a policy that downgrades the one "Critical" vuln -> this downgrades
		// the overall status to "High" since there are also several "High" vulns
		//
		// Most of the following testcases are alterations of this policy.
		expect(models.HighSeverity, keppel.SecurityScanPolicy{
			RepositoryRx:      ".*",
			VulnerabilityIDRx: "CVE-2019-8457",
			Action: keppel.SecurityScanPolicyAction{
				Assessment: "we accept the risk",
				Severity:   models.LowSeverity,
			},
		})

		// test Action.Ignore -> same result
		expect(models.HighSeverity, keppel.SecurityScanPolicy{
			RepositoryRx:      ".*",
			VulnerabilityIDRx: "CVE-2019-8457",
			Action: keppel.SecurityScanPolicyAction{
				Assessment: "we accept the risk",
				Ignore:     true,
			},
		})

		// test RepositoryRx
		expect(models.CriticalSeverity, keppel.SecurityScanPolicy{
			RepositoryRx:      "bar", // does not match our test repo
			VulnerabilityIDRx: "CVE-2019-8457",
			Action: keppel.SecurityScanPolicyAction{
				Assessment: "we accept the risk",
				Severity:   models.LowSeverity,
			},
		})

		// test NegativeRepositoryRx
		expect(models.CriticalSeverity, keppel.SecurityScanPolicy{
			RepositoryRx:         ".*",
			NegativeRepositoryRx: "foo", // matches our test repo
			VulnerabilityIDRx:    "CVE-2019-8457",
			Action: keppel.SecurityScanPolicyAction{
				Assessment: "we accept the risk",
				Severity:   models.LowSeverity,
			},
		})

		// test NegativeVulnerabilityIDRx
		expect(models.CriticalSeverity, keppel.SecurityScanPolicy{
			RepositoryRx:              ".*",
			VulnerabilityIDRx:         ".*",
			NegativeVulnerabilityIDRx: "CVE-2019-8457",
			Action: keppel.SecurityScanPolicyAction{
				Assessment: "we accept the risk",
				Severity:   models.LowSeverity,
			},
		})

		// test ExceptFixReleased on its own (the highest vulnerability with a
		// released fix is "High")
		expect(models.HighSeverity, keppel.SecurityScanPolicy{
			RepositoryRx:      ".*",
			VulnerabilityIDRx: ".*",
			ExceptFixReleased: true,
			Action: keppel.SecurityScanPolicyAction{
				Assessment: "we can only update if a fix is available",
				Ignore:     true,
			},
		})

		// test ExceptFixReleased together with an ignore of all high-severity fixed
		// vulns (the next highest vulnerability with a released fix is "Medium")
		expect(models.MediumSeverity,
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
				VulnerabilityIDRx: "CVE-2022-29458", // matches vulnerabilities in multiple packages
				Action: keppel.SecurityScanPolicyAction{
					Assessment: "will fix tomorrow, I swear",
					Severity:   models.LowSeverity,
				},
			},
		)
	})
}

func TestCheckTrivySecurityStatusWithEOSL(t *testing.T) {
	test.WithRoundTripper(func(_ *test.RoundTripper) {
		j, s := setup(t, test.WithTrivyDouble)
		tr, _ := easypg.NewTracker(t, s.DB.Db)
		trivyJob := j.CheckTrivySecurityStatusJob(s.Registry)

		// upload an example image
		image := test.GenerateImage(test.GenerateExampleLayer(4))
		manifest := image.MustUpload(t, s, fooRepoRef, "latest")
		tr.DBChanges().Ignore()
		s.TrivyDouble.ReportFixtures[image.ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-eosl.json"

		// there are vulnerabilities up to "Critical", but since the base distro is
		// EOSL, the vulnerability status gets overridden into "Rotten"
		s.Clock.StepBy(1 * time.Hour)
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), nil)
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), sql.ErrNoRows)
		tr.DBChanges().AssertEqualf(`
			UPDATE blobs SET blocks_vuln_scanning = FALSE WHERE id = 1 AND account_name = 'test1' AND digest = '%[1]s';
			UPDATE trivy_security_info SET vuln_status = '%[2]s', next_check_at = NULL, checked_at = %[3]d, check_duration_secs = 0, has_enriched_report = TRUE WHERE repo_id = 1 AND digest = '%[4]s';
		`, image.Layers[0].Digest, models.RottenVulnerabilityStatus, s.Clock.Now().Unix(), image.Manifest.Digest)

		s.ExpectTrivyReportExistsInStorage(t, manifest, "json", assert.JSONFixtureFile("fixtures/trivy/report-eosl-with-enriched.json"))
	})
}

func TestManifestValidationJobWithoutPlatform(t *testing.T) {
	j, s := setup(t)
	tr, _ := easypg.NewTracker(t, s.DB.Db)
	validateManifestJob := j.ManifestValidationJob(s.Registry)

	image := test.GenerateImage(test.GenerateExampleLayer(1))
	image.MustUpload(t, s, fooRepoRef, "")

	manifestListBytes, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     imgspecv1.MediaTypeImageIndex,
		"manifests": []map[string]any{{
			"mediaType":    image.Manifest.MediaType,
			"size":         len(image.Manifest.Contents),
			"digest":       image.Manifest.Digest,
			"artifactType": "application/spdx+json",
		}},
	})
	if err != nil {
		panic(err.Error())
	}

	imageList := test.ImageList{
		Manifest: test.Bytes{
			Contents:  manifestListBytes,
			Digest:    digest.Canonical.FromBytes(manifestListBytes),
			MediaType: imgspecv1.MediaTypeImageIndex,
		},
	}
	imageList.MustUpload(t, s, fooRepoRef, "")
	tr.DBChanges().Ignore()

	// validation should be happy and despite the missing platform because the manifest got skipped
	s.Clock.StepBy(36 * time.Hour)
	assert.ErrEqual(t, validateManifestJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, validateManifestJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, validateManifestJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().AssertEqualf(`
			UPDATE manifests SET next_validation_at = %d WHERE repo_id = 1 AND digest = '%s';
			UPDATE manifests SET next_validation_at = %d WHERE repo_id = 1 AND digest = '%s';
		`,
		s.Clock.Now().Add(models.ManifestValidationInterval).Unix(), imageList.Manifest.Digest,
		s.Clock.Now().Add(models.ManifestValidationInterval).Unix(), image.Manifest.Digest,
	)
}

func TestVulnerabilityStatusChanged(t *testing.T) {
	test.WithRoundTripper(func(_ *test.RoundTripper) {
		j, s := setup(t, test.WithTrivyDouble)
		s.Clock.StepBy(1 * time.Hour)

		// setup two image manifests with just one content layer (we don't really care about
		// the content since our Trivy double doesn't care either)
		images := make([]test.Image, 2)
		imageManifests := make([]models.Manifest, 2)
		for idx := range images {
			images[idx] = test.GenerateImage(test.GenerateExampleLayer(int64(idx)))
			imageManifests[idx] = images[idx].MustUpload(t, s, fooRepoRef, "")
		}

		// also setup an image list manifest containing those images (so that we have
		// some manifest-manifest refs to play with)
		imageList := test.GenerateImageList(images[0], images[1])
		imageListManifest := imageList.MustUpload(t, s, fooRepoRef, "")

		tr, tr0 := easypg.NewTracker(t, s.DB.Db)
		tr0.AssertEqualToFile("fixtures/vulnerability-changed-at-setup.sql")

		trivyJob := j.CheckTrivySecurityStatusJob(s.Registry)

		// check that security check updates vulnerability status
		s.TrivyDouble.ReportFixtures[images[0].ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-vulnerable.json"
		s.TrivyDouble.ReportFixtures[images[1].ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-clean.json"
		s.Clock.StepBy(5 * time.Minute)
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), nil)
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), sql.ErrNoRows)
		tr.DBChanges().AssertEqualf(`
			UPDATE blobs SET blocks_vuln_scanning = FALSE WHERE id = 1 AND account_name = 'test1' AND digest = '%[7]s';
			UPDATE blobs SET blocks_vuln_scanning = FALSE WHERE id = 3 AND account_name = 'test1' AND digest = '%[8]s';
			UPDATE trivy_security_info SET vuln_status = 'Critical', next_check_at = %[5]d, checked_at = %[4]d, check_duration_secs = 0, has_enriched_report = TRUE WHERE repo_id = 1 AND digest = '%[1]s';
			UPDATE trivy_security_info SET next_check_at = %[5]d, checked_at = %[4]d, check_duration_secs = 0 WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE trivy_security_info SET vuln_status = 'Clean', next_check_at = %[5]d, checked_at = %[4]d, check_duration_secs = 0, has_enriched_report = TRUE WHERE repo_id = 1 AND digest = '%[3]s';
		`, images[0].Manifest.Digest, imageList.Manifest.Digest, images[1].Manifest.Digest,
			s.Clock.Now().Unix(), s.Clock.Now().Add(60*time.Minute).Unix(), s.Clock.Now().Add(24*time.Hour).Unix(),
			images[0].Layers[0].Digest, images[1].Layers[0].Digest)

		// for scannable images, a report should now be cached in storage
		s.ExpectTrivyReportExistsInStorage(t, imageManifests[0], "json", assert.JSONFixtureFile("fixtures/trivy/report-vulnerable.json"))
		s.ExpectTrivyReportExistsInStorage(t, imageManifests[1], "json", assert.JSONFixtureFile("fixtures/trivy/report-clean.json"))
		s.ExpectTrivyReportMissingInStorage(t, imageListManifest, "json")

		// check that a changed vulnerability status does not have any unexpected side effects
		s.TrivyDouble.ReportFixtures[images[1].ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-vulnerable.json"
		s.Clock.StepBy(1 * time.Hour)
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), nil)
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), sql.ErrNoRows)
		tr.DBChanges().AssertEqualf(`
			UPDATE trivy_security_info SET next_check_at = %[5]d, checked_at = %[4]d WHERE repo_id = 1 AND digest = '%[1]s';
			UPDATE trivy_security_info SET vuln_status = 'Critical', next_check_at = %[5]d, checked_at = %[4]d WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE trivy_security_info SET vuln_status = 'Critical', next_check_at = %[5]d, checked_at = %[4]d, vuln_status_changed_at = %[4]d WHERE repo_id = 1 AND digest = '%[3]s';
		`, images[0].Manifest.Digest, imageList.Manifest.Digest, images[1].Manifest.Digest,
			s.Clock.Now().Unix(), s.Clock.Now().Add(1*time.Hour).Unix(),
		)
	})
}

func TestCheckTrivySecurityStatusWithAccountBeingDeleted(t *testing.T) {
	test.WithRoundTripper(func(_ *test.RoundTripper) {
		j, s := setup(t, test.WithTrivyDouble)
		tr, _ := easypg.NewTracker(t, s.DB.Db)
		trivyJob := j.CheckTrivySecurityStatusJob(s.Registry)

		// upload an example image and change account to being deleted
		image := test.GenerateImage(test.GenerateExampleLayer(4))
		image.MustUpload(t, s, fooRepoRef, "latest")
		_ = must.ReturnT(j.db.Exec(`UPDATE accounts SET is_deleting = TRUE WHERE name = 'test1'`))(t)
		tr.DBChanges().Ignore()
		s.TrivyDouble.ReportFixtures[image.ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-vulnerable.json"

		// simulate a transient error which has no effect because the account is being deleted
		s.Clock.StepBy(1 * time.Hour)
		s.TrivyDouble.ReportError[image.ImageRef(s, fooRepoRef)] = true
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), sql.ErrNoRows)
		tr.DBChanges().AssertEmpty()
		s.TrivyDouble.ReportError[image.ImageRef(s, fooRepoRef)] = false

		// accounts that are being deleted should be skipped
		s.Clock.StepBy(1 * time.Hour)
		assert.ErrEqual(t, trivyJob.ProcessOne(s.Ctx), sql.ErrNoRows)
		tr.DBChanges().AssertEmpty()
	})
}

func TestCheckTrivySecurityStatusBeingDeleted(t *testing.T) {
	test.WithRoundTripper(func(_ *test.RoundTripper) {
		j, s := setup(t, test.WithTrivyDouble)
		tr, _ := easypg.NewTracker(t, s.DB.Db)
		trivyJob := j.CheckTrivySecurityStatusJob(s.Registry)
		sweepStorageJob := j.StorageSweepJob(s.Registry)

		// upload an example image and generate a trivy security report
		image := test.GenerateImage(test.GenerateExampleLayer(4))
		image.MustUpload(t, s, fooRepoRef, "latest")
		s.TrivyDouble.ReportFixtures[image.ImageRef(s, fooRepoRef)] = "fixtures/trivy/report-vulnerable.json"
		must.Succeed(trivyJob.ProcessOne(s.Ctx))
		tr.DBChanges().Ignore()

		// delete a manifest
		s.Clock.StepBy(30 * time.Minute)
		test.MustExec(t, s.DB, `DELETE FROM manifests WHERE digest = $1`, image.Manifest.Digest)
		tr.DBChanges().AssertEqualf(`
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[1]s' AND blob_id = 1;
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[1]s' AND blob_id = 2;
			DELETE FROM manifest_contents WHERE repo_id = 1 AND digest = '%[1]s';
			DELETE FROM manifests WHERE repo_id = 1 AND digest = '%[1]s';
			DELETE FROM tags WHERE repo_id = 1 AND name = 'latest';
			DELETE FROM trivy_security_info WHERE repo_id = 1 AND digest = '%[1]s';
		`, image.Manifest.Digest)

		// mark manifest for sweep
		s.Clock.StepBy(30 * time.Minute)
		must.Succeed(sweepStorageJob.ProcessOne(s.Ctx))
		assert.ErrEqual(t, sweepStorageJob.ProcessOne(s.Ctx), sql.ErrNoRows)
		tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET next_storage_sweep_at = %[1]d WHERE name = 'test1';
			INSERT INTO unknown_manifests (account_name, repo_name, digest, can_be_deleted_at) VALUES ('test1', 'foo', '%[2]s', %[3]d);
			INSERT INTO unknown_trivy_reports (account_name, repo_name, digest, format, can_be_deleted_at) VALUES ('test1', 'foo', '%[2]s', 'json', %[3]d);
		`, s.Clock.Now().Add(6*time.Hour).Unix(), image.Manifest.Digest, s.Clock.Now().Add(4*time.Hour).Unix())

		// clean up manifest for the deleted account
		s.Clock.StepBy(12 * time.Hour)
		must.Succeed(sweepStorageJob.ProcessOne(s.Ctx))
		assert.ErrEqual(t, sweepStorageJob.ProcessOne(s.Ctx), sql.ErrNoRows)
		tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET next_storage_sweep_at = %[1]d WHERE name = 'test1';
			DELETE FROM unknown_manifests WHERE account_name = 'test1' AND repo_name = 'foo' AND digest = '%[2]s';
			DELETE FROM unknown_trivy_reports WHERE account_name = 'test1' AND repo_name = 'foo' AND digest = '%[2]s' AND format = 'json';
		`, s.Clock.Now().Add(6*time.Hour).Unix(), image.Manifest.Digest)
	})
}
