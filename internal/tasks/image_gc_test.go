// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"

	"github.com/sapcc/keppel/internal/test"
)

// TestGCUntaggedImages is the original image GC testcase. It tests with just a
// single GC policy that deletes untagged images, but goes through all the
// phases of a manifest's lifecycle (as far as GC is concerned), covering some
// corner cases, such as no policies matching on a repo at all, or
// protected_by_recent_upload.
func TestGCUntaggedImages(t *testing.T) {
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)

	// setup GC policy for test
	matchingGCPolicyJSON := `{"match_repository":".*","only_untagged":true,"action":"delete"}`
	matchingGCPoliciesJSON := fmt.Sprintf("[%s]", matchingGCPolicyJSON)
	test.MustExec(t, s.DB,
		`UPDATE accounts SET gc_policies_json = $1`,
		matchingGCPoliciesJSON,
	)

	// store two images, one tagged, one untagged
	images := []test.Image{
		test.GenerateImage(test.GenerateExampleLayer(0)),
		test.GenerateImage(test.GenerateExampleLayer(1)),
	}
	images[0].MustUpload(t, s, fooRepoRef, "first")
	images[1].MustUpload(t, s, fooRepoRef, "")

	garbageJob := j.ManifestGarbageCollectionJob(s.Registry)

	// GC should not do anything right now because newly-pushed images are
	// protected (to avoid deleting images that a client is about to tag)
	assert.ErrEqual(t, garbageJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, garbageJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr, _ := easypg.NewTracker(t, s.DB.Db)

	// setup GC policy that does not match
	s.Clock.StepBy(2 * time.Hour)
	ineffectiveGCPoliciesJSON := `[{"match_repository":".*","except_repository":"foo","only_untagged":true,"action":"delete"}]`
	test.MustExec(t, s.DB,
		`UPDATE accounts SET gc_policies_json = $1`,
		ineffectiveGCPoliciesJSON,
	)

	// GC should only update the next_gc_at timestamp and the gc_status_json field
	// (indicating that no policies match), and otherwise not do anything
	assert.ErrEqual(t, garbageJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, garbageJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET gc_policies_json = '%[1]s' WHERE name = 'test1';
			UPDATE manifests SET gc_status_json = '{"relevant_policies":[]}' WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE manifests SET gc_status_json = '{"relevant_policies":[]}' WHERE repo_id = 1 AND digest = '%[3]s';
			UPDATE repos SET next_gc_at = %[4]d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
		`,
		ineffectiveGCPoliciesJSON,
		images[0].Manifest.Digest,
		images[1].Manifest.Digest,
		s.Clock.Now().Add(1*time.Hour).Unix(),
	)

	// setup GC policy that matches
	s.Clock.StepBy(2 * time.Hour)
	test.MustExec(t, s.DB,
		`UPDATE accounts SET gc_policies_json = $1`,
		matchingGCPoliciesJSON,
	)
	// however now there's also a tagged image list referencing it
	imageList := test.GenerateImageList(images[0], images[1])
	imageList.MustUpload(t, s, fooRepoRef, "list")
	tr.DBChanges().Ignore()

	// GC should not delete the untagged image since it's referenced by the tagged list image
	assert.ErrEqual(t, garbageJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, garbageJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().AssertEqualf(`
			UPDATE manifests SET gc_status_json = '{"protected_by_parent":"%[1]s"}' WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE manifests SET gc_status_json = '{"protected_by_recent_upload":true}' WHERE repo_id = 1 AND digest = '%[1]s';
			UPDATE manifests SET gc_status_json = '{"protected_by_parent":"%[1]s"}' WHERE repo_id = 1 AND digest = '%[3]s';
			UPDATE repos SET next_gc_at = %[4]d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
		`,
		imageList.Manifest.Digest,
		images[0].Manifest.Digest,
		images[1].Manifest.Digest,
		s.Clock.Now().Add(1*time.Hour).Unix(),
	)

	// delete the image list manifest
	s.Clock.StepBy(2 * time.Hour)
	test.MustExec(t, s.DB,
		`DELETE FROM manifests WHERE digest = $1`,
		imageList.Manifest.Digest,
	)
	tr.DBChanges().Ignore()
	s.Auditor.IgnoreEventsUntilNow()

	// GC should now delete the untagged image since nothing references it anymore
	assert.ErrEqual(t, garbageJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, garbageJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().AssertEqualf(`
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[2]s' AND blob_id = 3;
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[2]s' AND blob_id = 4;
			DELETE FROM manifest_contents WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE manifests SET gc_status_json = '{"relevant_policies":%[3]s}' WHERE repo_id = 1 AND digest = '%[1]s';
			DELETE FROM manifests WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE repos SET next_gc_at = %[4]d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
			DELETE FROM trivy_security_info WHERE repo_id = 1 AND digest = '%[2]s';
		`,
		images[0].Manifest.Digest,
		images[1].Manifest.Digest,
		matchingGCPoliciesJSON,
		s.Clock.Now().Add(1*time.Hour).Unix(),
	)

	// there should be an audit event for when GC deletes an image
	s.Auditor.ExpectEvents(t, cadf.Event{
		RequestPath: janitorDummyRequest.URL.String(),
		Action:      cadf.DeleteAction,
		Outcome:     "success",
		Reason:      test.CADFReasonOK,
		Target: cadf.Resource{
			TypeURI:   "docker-registry/account/repository/manifest",
			Name:      "test1/foo@" + images[1].Manifest.Digest.String(),
			ID:        images[1].Manifest.Digest.String(),
			ProjectID: "test1authtenant",
		},
		Initiator: cadf.Resource{
			TypeURI: "service/docker-registry/janitor-task",
			ID:      "policy-driven-gc",
			Name:    "policy-driven-gc",
			Domain:  "keppel",
			Attachments: []cadf.Attachment{{
				Name:    "gc-policy",
				TypeURI: "mime:application/json",
				Content: matchingGCPolicyJSON,
			}},
		},
	})
}

// TestGCMatchOnTag exercises all valid combinations of match_tag and except_tag.
// (The only_untagged match was already tested in TestGCUntaggedImages.)
func TestGCMatchOnTag(t *testing.T) {
	j, s := setup(t)

	images := []test.Image{
		test.GenerateImage(test.GenerateExampleLayer(0)),
		test.GenerateImage(test.GenerateExampleLayer(1)),
		test.GenerateImage(test.GenerateExampleLayer(2)),
		test.GenerateImage(test.GenerateExampleLayer(3)),
	}
	// each image gets uploaded with four tags, e.g. "zerozero" through "zerothree" for images[0]
	words := []string{"zero", "one", "two", "three"}
	for idx, image := range images {
		firstWord := words[idx]
		for _, secondWord := range words {
			image.MustUpload(t, s, fooRepoRef, firstWord+secondWord)
		}
	}

	// skip an hour to avoid protected_by_recent_upload
	s.Clock.StepBy(1 * time.Hour)

	// setup GC policies such that the deletion policy would affect all images,
	// but the tag-matching policies protect some of the images from deletion;
	protectingGCPolicyJSON1 := `{"match_repository":"foo","match_tag":"one.*","action":"protect"}`
	protectingGCPolicyJSON2 := `{"match_repository":"foo","match_tag":".*two","except_tag":"[zot][^w].*","action":"protect"}`
	protectingGCPolicyJSON3 := `{"match_repository":"foo","except_tag":"zero.*|one.*|two.*","action":"protect"}`
	deletingGCPolicyJSON := `{"match_repository":".*","time_constraint":{"on":"pushed_at","older_than":{"value":30,"unit":"m"}},"action":"delete"}`
	test.MustExec(t, s.DB,
		`UPDATE accounts SET gc_policies_json = $1`,
		fmt.Sprintf("[%s,%s,%s,%s]",
			protectingGCPolicyJSON1,
			protectingGCPolicyJSON2,
			protectingGCPolicyJSON3,
			deletingGCPolicyJSON,
		),
	)
	tr, _ := easypg.NewTracker(t, s.DB.Db)

	garbageJob := j.ManifestGarbageCollectionJob(s.Registry)

	// protectingGCPolicyJSON1 protects images[1], and so forth, so only images[0]
	// should end up getting deleted (NOTE: in the DB diff, the manifests are not
	// in order because easypg orders them by primary key, i.e. by digest)
	assert.ErrEqual(t, garbageJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, garbageJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().AssertEqualf(`
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[1]s' AND blob_id = 1;
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[1]s' AND blob_id = 2;
			DELETE FROM manifest_contents WHERE repo_id = 1 AND digest = '%[1]s';
			DELETE FROM manifests WHERE repo_id = 1 AND digest = '%[1]s';
			UPDATE manifests SET gc_status_json = '{"protected_by_policy":%[6]s}' WHERE repo_id = 1 AND digest = '%[3]s';
			UPDATE manifests SET gc_status_json = '{"protected_by_policy":%[7]s}' WHERE repo_id = 1 AND digest = '%[4]s';
			UPDATE manifests SET gc_status_json = '{"protected_by_policy":%[5]s}' WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE repos SET next_gc_at = %[8]d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
			DELETE FROM tags WHERE repo_id = 1 AND name = 'zeroone';
			DELETE FROM tags WHERE repo_id = 1 AND name = 'zerothree';
			DELETE FROM tags WHERE repo_id = 1 AND name = 'zerotwo';
			DELETE FROM tags WHERE repo_id = 1 AND name = 'zerozero';
			DELETE FROM trivy_security_info WHERE repo_id = 1 AND digest = '%[1]s';
		`,
		images[0].Manifest.Digest,
		images[1].Manifest.Digest,
		images[2].Manifest.Digest,
		images[3].Manifest.Digest,
		protectingGCPolicyJSON1,
		protectingGCPolicyJSON2,
		protectingGCPolicyJSON3,
		s.Clock.Now().Add(1*time.Hour).Unix(),
	)
}

// TestGCProtectOldestAndNewest exercises the various kinds of time constraints.
// The first pass ("byCount") uses "oldest" and "newest" time constraints,
// whereas the second pass ("byThreshold") uses "older_than" and "newer_than"
// time constraints.
//
// Since both tests are otherwise very similar, they have been merged into one
// Test function to avoid code duplication.
func TestGCProtectOldestAndNewest(t *testing.T) {
	for _, strategy := range []string{"byCount", "byThreshold"} {
		j, s := setup(t)

		// upload a few test images
		images := make([]test.Image, 6)
		for idx := range images {
			image := test.GenerateImage(test.GenerateExampleLayer(int64(idx)))
			image.MustUpload(t, s, fooRepoRef, "")
			images[idx] = image
		}

		// skip an hour to avoid protected_by_recent_upload, and also to make sure
		// that all the last_pulled_at values that we set below are in the past (it
		// should not matter, but let's be sure)
		s.Clock.StepBy(1 * time.Hour)

		// set up last_pulled_at in a precise order, including a NULL value to later
		// check that NULL gets coerced into time.Unix(0, 0)
		for idx, image := range images {
			if idx == 0 {
				test.MustExec(t, s.DB,
					`UPDATE manifests SET last_pulled_at = NULL WHERE digest = $1`,
					image.Manifest.Digest,
				)
			} else {
				test.MustExec(t, s.DB,
					`UPDATE manifests SET last_pulled_at = $2 WHERE digest = $1`,
					image.Manifest.Digest,
					j.timeNow().Add(-10*time.Minute*time.Duration(len(images)-idx)),
				)
			}
		}

		// setup GC policies such that images[0:2] are protected by "oldest/older_than"
		// and images[4:5] are protected by "newest/newer_than"...
		protectingGCPolicyJSON1 := `{"match_repository":".*","time_constraint":{"on":"last_pulled_at","oldest":3},"action":"protect"}`
		protectingGCPolicyJSON2 := `{"match_repository":".*","time_constraint":{"on":"last_pulled_at","newest":2},"action":"protect"}`
		if strategy == "byThreshold" { // instead of "byCount"
			protectingGCPolicyJSON1 = `{"match_repository":".*","time_constraint":{"on":"last_pulled_at","older_than":{"value":35,"unit":"m"}},"action":"protect"}`
			protectingGCPolicyJSON2 = `{"match_repository":".*","time_constraint":{"on":"last_pulled_at","newer_than":{"value":25,"unit":"m"}},"action":"protect"}`
		}
		deletingGCPolicyJSON := `{"match_repository":".*","time_constraint":{"on":"pushed_at","older_than":{"value":30,"unit":"m"}},"action":"delete"}`
		test.MustExec(t, s.DB,
			`UPDATE accounts SET gc_policies_json = $1`,
			fmt.Sprintf("[%s,%s,%s]",
				protectingGCPolicyJSON1,
				protectingGCPolicyJSON2,
				deletingGCPolicyJSON,
			),
		)
		tr, _ := easypg.NewTracker(t, s.DB.Db)

		garbageJob := j.ManifestGarbageCollectionJob(s.Registry)

		// ...so only images[3] gets garbage-collected (NOTE: in the DB diff, the
		// manifests are not in order because easypg orders them by primary key, i.e.
		// by digest)
		assert.ErrEqual(t, garbageJob.ProcessOne(s.Ctx), nil)
		assert.ErrEqual(t, garbageJob.ProcessOne(s.Ctx), sql.ErrNoRows)
		tr.DBChanges().AssertEqualf(`
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[4]s' AND blob_id = 7;
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[4]s' AND blob_id = 8;
			DELETE FROM manifest_contents WHERE repo_id = 1 AND digest = '%[4]s';
			UPDATE manifests SET gc_status_json = '{"protected_by_policy":%[7]s}' WHERE repo_id = 1 AND digest = '%[1]s';
			UPDATE manifests SET gc_status_json = '{"protected_by_policy":%[7]s}' WHERE repo_id = 1 AND digest = '%[3]s';
			DELETE FROM manifests WHERE repo_id = 1 AND digest = '%[4]s';
			UPDATE manifests SET gc_status_json = '{"protected_by_policy":%[8]s}' WHERE repo_id = 1 AND digest = '%[6]s';
			UPDATE manifests SET gc_status_json = '{"protected_by_policy":%[7]s}' WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE manifests SET gc_status_json = '{"protected_by_policy":%[8]s}' WHERE repo_id = 1 AND digest = '%[5]s';
			UPDATE repos SET next_gc_at = %[9]d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
			DELETE FROM trivy_security_info WHERE repo_id = 1 AND digest = '%[4]s';
		`,
			images[0].Manifest.Digest,
			images[1].Manifest.Digest,
			images[2].Manifest.Digest,
			images[3].Manifest.Digest,
			images[4].Manifest.Digest,
			images[5].Manifest.Digest,
			protectingGCPolicyJSON1,
			protectingGCPolicyJSON2,
			s.Clock.Now().Add(1*time.Hour).Unix(),
		)
	}
}

// TestGCProtectComesTooLate checks that a "protect" policy is ineffective if an
// image has already been removed by an earlier "delete" policy.
func TestGCProtectComesTooLate(t *testing.T) {
	j, s := setup(t)

	// upload some test images
	images := []test.Image{
		test.GenerateImage(test.GenerateExampleLayer(0)),
		test.GenerateImage(test.GenerateExampleLayer(1)),
	}
	images[0].MustUpload(t, s, fooRepoRef, "earliest")
	images[1].MustUpload(t, s, fooRepoRef, "latest")

	// skip an hour to avoid protected_by_recent_upload
	s.Clock.StepBy(1 * time.Hour)

	// setup GC policies such that images[0] is properly protected, but the protecting policy for images[1] comes too late
	protectingGCPolicyJSON1 := `{"match_repository":".*","match_tag":"earliest","action":"protect"}`
	protectingGCPolicyJSON2 := `{"match_repository":".*","match_tag":"latest","action":"protect"}`
	deletingGCPolicyJSON := `{"match_repository":".*","time_constraint":{"on":"pushed_at","older_than":{"value":30,"unit":"m"}},"action":"delete"}`
	test.MustExec(t, s.DB,
		`UPDATE accounts SET gc_policies_json = $1`,
		fmt.Sprintf("[%s,%s,%s]",
			protectingGCPolicyJSON1,
			deletingGCPolicyJSON,
			protectingGCPolicyJSON2,
		),
	)
	tr, _ := easypg.NewTracker(t, s.DB.Db)

	garbageJob := j.ManifestGarbageCollectionJob(s.Registry)

	// therefore, images[1] gets deleted
	assert.ErrEqual(t, garbageJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, garbageJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().AssertEqualf(`
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[2]s' AND blob_id = 3;
			DELETE FROM manifest_blob_refs WHERE repo_id = 1 AND digest = '%[2]s' AND blob_id = 4;
			DELETE FROM manifest_contents WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE manifests SET gc_status_json = '{"protected_by_policy":%[3]s}' WHERE repo_id = 1 AND digest = '%[1]s';
			DELETE FROM manifests WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE repos SET next_gc_at = %[4]d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
			DELETE FROM tags WHERE repo_id = 1 AND name = 'latest';
			DELETE FROM trivy_security_info WHERE repo_id = 1 AND digest = '%[2]s';
		`,
		images[0].Manifest.Digest,
		images[1].Manifest.Digest,
		protectingGCPolicyJSON1,
		s.Clock.Now().Add(1*time.Hour).Unix(),
	)
}

func TestGCProtectSubject(t *testing.T) {
	j, s := setup(t)

	image := test.GenerateOCIImage(test.OCIArgs{
		ConfigMediaType: imgspecv1.MediaTypeImageManifest,
	})
	image.MustUpload(t, s, fooRepoRef, "latest")

	subjectManifest := test.GenerateOCIImage(test.OCIArgs{
		ConfigMediaType: imgspecv1.MediaTypeImageManifest,
		SubjectDigest:   image.Manifest.Digest,
	})
	subjectManifest.MustUpload(t, s, fooRepoRef, strings.ReplaceAll(image.Manifest.Digest.String(), ":", "-"))

	deletingGCPolicyJSON := `[{"match_repository":".*","time_constraint":{"on":"pushed_at","older_than":{"value":2,"unit":"h"}},"action":"delete"}]`
	test.MustExec(t, s.DB, `UPDATE accounts SET gc_policies_json = $1`, deletingGCPolicyJSON)

	tr, _ := easypg.NewTracker(t, s.DB.Db)
	garbageCollectionJob := j.ManifestGarbageCollectionJob(s.Registry)

	// skip an hour to avoid protected_by_recent_upload
	s.Clock.StepBy(1 * time.Hour)

	// nothing should be deleted here
	assert.ErrEqual(t, garbageCollectionJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, garbageCollectionJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().AssertEqualf(`
			UPDATE manifests SET gc_status_json = '{"protected_by_subject":"%[1]s"}' WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE manifests SET gc_status_json = '{"relevant_policies":%[3]s}' WHERE repo_id = 1 AND digest = '%[1]s';
			UPDATE repos SET next_gc_at = %[4]d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
		`,
		image.Manifest.Digest, subjectManifest.Manifest.Digest, deletingGCPolicyJSON, s.Clock.Now().Add(1*time.Hour).Unix(),
	)
}

func TestTagPolicyProtectsFromGCManifest(t *testing.T) {
	j, s := setup(t)

	image := test.GenerateImage(test.GenerateExampleLayer(0))
	image.MustUpload(t, s, fooRepoRef, "test")

	deletingTagPolicyJSON := `{"match_repository":".*","block_delete":true}`
	test.MustExec(t, s.DB, `UPDATE accounts SET tag_policies_json = $1`, "["+deletingTagPolicyJSON+"]")

	deletingGCPolicyJSON := `[{"match_repository":".*","time_constraint":{"on":"pushed_at","older_than":{"value":30,"unit":"m"}},"action":"delete"}]`
	test.MustExec(t, s.DB, `UPDATE accounts SET gc_policies_json = $1`, deletingGCPolicyJSON)

	tr, _ := easypg.NewTracker(t, s.DB.Db)
	garbageJob := j.ManifestGarbageCollectionJob(s.Registry)

	// skip an hour to avoid protected_by_recent_upload
	s.Clock.StepBy(1 * time.Hour)

	// nothing should be deleted here
	assert.ErrEqual(t, garbageJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, garbageJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().AssertEqualf(`
			UPDATE manifests SET gc_status_json = '{"protected_by_tag_policy":%[1]s}' WHERE repo_id = 1 AND digest = '%[2]s';
			UPDATE repos SET next_gc_at = %[3]d WHERE id = 1 AND account_name = 'test1' AND name = 'foo';
		`,
		deletingTagPolicyJSON, image.Manifest.Digest, s.Clock.Now().Add(1*time.Hour).Unix(),
	)
}
