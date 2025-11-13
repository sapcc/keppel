// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	. "github.com/majewsky/gg/option"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/processor"
	"github.com/sapcc/keppel/internal/test"
	"github.com/sapcc/keppel/internal/trivy"
)

func TestAccountManagementBasic(t *testing.T) {
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)

	tr, tr0 := easypg.NewTracker(t, s.DB.Db)
	tr0.Ignore()
	managedAccountsJob := j.EnforceManagedAccountsJob(s.Registry)
	deleteAccountsJob := j.DeleteAccountsJob(s.Registry)

	// we haven't configured any account, so it should do nothing
	s.AMD.ConfigPath = "./fixtures/account_management_empty.json"
	assert.ErrEqual(t, managedAccountsJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().AssertEmpty()

	// configure an account to create
	s.AMD.ConfigPath = "../drivers/basic/fixtures/account_management.json"
	s.Clock.StepBy(1 * time.Hour)

	// we only have one account defined which should be created
	assert.ErrEqual(t, managedAccountsJob.ProcessOne(s.Ctx), nil)
	// since we are enforcing that account, no error is returned
	assert.ErrEqual(t, managedAccountsJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().AssertEqualf(`
			INSERT INTO accounts (name, auth_tenant_id, external_peer_url, gc_policies_json, security_scan_policies_json, rbac_policies_json, is_managed, next_enforcement_at, rule_for_manifest) VALUES ('abcde', '12345', 'registry-tertiary.example.org', '[{"match_repository":".*/database","except_repository":"archive/.*","time_constraint":{"on":"pushed_at","newer_than":{"value":6,"unit":"h"}},"action":"protect"},{"match_repository":".*","only_untagged":true,"action":"delete"}]', '[{"match_repository":".*","match_vulnerability_id":".*","except_fix_released":true,"action":{"assessment":"risk accepted: vulnerabilities without an available fix are not actionable","ignore":true}}]', '[{"match_repository":"library/.*","permissions":["anonymous_pull"]},{"match_repository":"library/alpine","match_username":".*@tenant2","permissions":["pull","push"]}]', TRUE, %d, '''important-label'' in labels && ''some-label'' in labels');
		`,
		s.Clock.Now().Add(1*time.Hour).Unix())

	// test if errors are propagated
	s.AMD.ConfigPath = "./fixtures/account_management_error.json"
	s.Clock.StepBy(2 * time.Hour)
	assert.ErrEqual(t, managedAccountsJob.ProcessOne(s.Ctx), fmt.Sprintf("could not configure managed account %q: %s", "", processor.ErrAccountNameEmpty.Error()))
	tr.DBChanges().AssertEmpty()

	// and delete the account again
	s.AMD.ConfigPath = "./fixtures/account_management_empty.json"
	s.Clock.StepBy(2 * time.Hour)
	// now the account is being marked for deletion
	assert.ErrEqual(t, managedAccountsJob.ProcessOne(s.Ctx), nil)
	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET next_enforcement_at = %d, is_deleting = TRUE, next_deletion_attempt_at = %d WHERE name = 'abcde';
		`,
		s.Clock.Now().Add(1*time.Hour).Unix(),
		s.Clock.Now().Unix(),
	)

	// and we can immeadetaly delete it yet because it has no manifests and blobs attached to it
	s.Clock.StepBy(1 * time.Hour)
	assert.ErrEqual(t, deleteAccountsJob.ProcessOne(s.Ctx), nil)
	tr.DBChanges().AssertEqual(`
		DELETE FROM accounts WHERE name = 'abcde';
	`)
}

func TestAccountManagementWithReplicaCreation(t *testing.T) {
	test.WithRoundTripper(func(_ *test.RoundTripper) {
		_, s1 := setup(t)
		j2, s2 := setupReplica(t, s1, "on_first_use")

		tr, tr0 := easypg.NewTracker(t, s2.DB.Db)
		tr0.Ignore()

		// The setup already includes an account "test1" set up on both ends, but we
		// want to test the setup of a managed replica account, so we will use a
		// fresh account called "managed" instead.
		must.SucceedT(t, s1.DB.Insert(&models.Account{Name: "managed", AuthTenantID: "managedauthtenant"}))
		s1.FD.NextSubleaseTokenSecretToIssue = "thisisasecret"
		s2.FD.ValidSubleaseTokenSecrets["managed"] = "thisisasecret"

		// test seeding the replica on the secondary side
		s2.AMD.ConfigPath = "./fixtures/account_management_replica.json"
		job := j2.EnforceManagedAccountsJob(s2.Registry)
		assert.ErrEqual(t, job.ProcessOne(s2.Ctx), nil)

		// check that the replica was created
		tr.DBChanges().AssertEqualf(`
				INSERT INTO accounts (name, auth_tenant_id, upstream_peer_hostname, security_scan_policies_json, is_managed, next_enforcement_at) VALUES ('managed', 'managedauthtenant', 'registry.example.org', 'null', TRUE, %[1]d);
			`,
			s2.Clock.Now().Add(1*time.Hour).Unix(),
		)
	})
}

func TestAccountManagementWithComplexDeletion(t *testing.T) {
	j, s := setup(t)
	managedAccountsJob := j.EnforceManagedAccountsJob(s.Registry)
	deleteAccountsJob := j.DeleteAccountsJob(s.Registry)
	blobSweepJob := j.BlobSweepJob(s.Registry)

	tr, tr0 := easypg.NewTracker(t, s.DB.Db)
	tr0.Ignore()

	// create a managed account named "abcde"
	s.AMD.ConfigPath = "./fixtures/account_management_basic.json"
	assert.ErrEqual(t, managedAccountsJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, managedAccountsJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().Ignore()

	// give quota to its auth tenant
	must.SucceedT(t, s.DB.Insert(&models.Quotas{
		AuthTenantID:  "12345",
		ManifestCount: 100,
	}))

	// upload an image into that account
	repo := models.Repository{
		AccountName: "abcde",
		Name:        "foo",
	}
	image := test.GenerateImage(
		test.GenerateExampleLayer(1),
		test.GenerateExampleLayer(2),
	)
	image.MustUpload(t, s, repo, "latest")

	// also setup some manifest-manifest refs to test with
	images := make([]test.Image, 2)
	for idx := range images {
		images[idx] = test.GenerateImage(test.GenerateExampleLayer(int64(idx + 2)))
		images[idx].MustUpload(t, s, repo, "")
	}
	imageList := test.GenerateImageList(images[0], images[1])
	imageList.MustUpload(t, s, repo, "")
	tr.DBChanges().Ignore()

	// remove the managed account: this will set is_deleting, but nothing more
	s.AMD.ConfigPath = "./fixtures/account_management_empty.json"
	s.Clock.StepBy(2 * time.Hour)
	assert.ErrEqual(t, managedAccountsJob.ProcessOne(s.Ctx), nil)
	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET next_enforcement_at = %d, is_deleting = TRUE, next_deletion_attempt_at = %d WHERE name = 'abcde';
		`,
		s.Clock.Now().Add(1*time.Hour).Unix(),
		s.Clock.Now().Unix(),
	)

	// the deleteAccountsJob will delete:
	// - all manifests
	// - all repos (and thus blob mounts) will be deleted
	// - all blobs are marked for immediate deletion, and a blob GC is scheduled
	s.Clock.StepBy(1 * time.Minute)
	assert.ErrEqual(t, deleteAccountsJob.ProcessOne(s.Ctx), nil)
	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET next_blob_sweep_at = %[1]d, next_deletion_attempt_at = %[2]d WHERE name = 'abcde';
			DELETE FROM blob_mounts WHERE blob_id = 1 AND repo_id = 2;
			DELETE FROM blob_mounts WHERE blob_id = 2 AND repo_id = 2;
			DELETE FROM blob_mounts WHERE blob_id = 3 AND repo_id = 2;
			DELETE FROM blob_mounts WHERE blob_id = 4 AND repo_id = 2;
			DELETE FROM blob_mounts WHERE blob_id = 5 AND repo_id = 2;
			DELETE FROM blob_mounts WHERE blob_id = 6 AND repo_id = 2;
			UPDATE blobs SET can_be_deleted_at = %[1]d WHERE id = 1 AND account_name = 'abcde' AND digest = '%[3]s';
			UPDATE blobs SET can_be_deleted_at = %[1]d WHERE id = 2 AND account_name = 'abcde' AND digest = '%[4]s';
			UPDATE blobs SET can_be_deleted_at = %[1]d WHERE id = 3 AND account_name = 'abcde' AND digest = '%[5]s';
			UPDATE blobs SET can_be_deleted_at = %[1]d WHERE id = 4 AND account_name = 'abcde' AND digest = '%[6]s';
			UPDATE blobs SET can_be_deleted_at = %[1]d WHERE id = 5 AND account_name = 'abcde' AND digest = '%[7]s';
			UPDATE blobs SET can_be_deleted_at = %[1]d WHERE id = 6 AND account_name = 'abcde' AND digest = '%[8]s';
			DELETE FROM manifest_blob_refs WHERE repo_id = 2 AND digest = '%[9]s' AND blob_id = 2;
			DELETE FROM manifest_blob_refs WHERE repo_id = 2 AND digest = '%[9]s' AND blob_id = 4;
			DELETE FROM manifest_blob_refs WHERE repo_id = 2 AND digest = '%[10]s' AND blob_id = 5;
			DELETE FROM manifest_blob_refs WHERE repo_id = 2 AND digest = '%[10]s' AND blob_id = 6;
			DELETE FROM manifest_blob_refs WHERE repo_id = 2 AND digest = '%[11]s' AND blob_id = 1;
			DELETE FROM manifest_blob_refs WHERE repo_id = 2 AND digest = '%[11]s' AND blob_id = 2;
			DELETE FROM manifest_blob_refs WHERE repo_id = 2 AND digest = '%[11]s' AND blob_id = 3;
			DELETE FROM manifest_contents WHERE repo_id = 2 AND digest = '%[9]s';
			DELETE FROM manifest_contents WHERE repo_id = 2 AND digest = '%[10]s';
			DELETE FROM manifest_contents WHERE repo_id = 2 AND digest = '%[11]s';
			DELETE FROM manifest_contents WHERE repo_id = 2 AND digest = '%[12]s';
			DELETE FROM manifest_manifest_refs WHERE repo_id = 2 AND parent_digest = '%[12]s' AND child_digest = '%[9]s';
			DELETE FROM manifest_manifest_refs WHERE repo_id = 2 AND parent_digest = '%[12]s' AND child_digest = '%[10]s';
			DELETE FROM manifests WHERE repo_id = 2 AND digest = '%[9]s';
			DELETE FROM manifests WHERE repo_id = 2 AND digest = '%[10]s';
			DELETE FROM manifests WHERE repo_id = 2 AND digest = '%[11]s';
			DELETE FROM manifests WHERE repo_id = 2 AND digest = '%[12]s';
			DELETE FROM repos WHERE id = 2 AND account_name = 'abcde' AND name = 'foo';
			DELETE FROM tags WHERE repo_id = 2 AND name = 'latest';
			DELETE FROM trivy_security_info WHERE repo_id = 2 AND digest = '%[9]s';
			DELETE FROM trivy_security_info WHERE repo_id = 2 AND digest = '%[10]s';
			DELETE FROM trivy_security_info WHERE repo_id = 2 AND digest = '%[11]s';
			DELETE FROM trivy_security_info WHERE repo_id = 2 AND digest = '%[12]s';
		`,
		s.Clock.Now().Unix(),
		s.Clock.Now().Add(1*time.Minute).Unix(),
		image.Layers[0].Digest.String(),
		image.Layers[1].Digest.String(),
		image.Config.Digest.String(),
		images[0].Config.Digest.String(),
		images[1].Layers[0].Digest.String(),
		images[1].Config.Digest.String(),
		images[0].Manifest.Digest.String(),
		images[1].Manifest.Digest.String(),
		image.Manifest.Digest.String(),
		imageList.Manifest.Digest.String(),
	)

	// we need to run this twice because the common test setup includes another account that is irrelevant to this test
	s.Clock.StepBy(1 * time.Minute)
	assert.ErrEqual(t, blobSweepJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, blobSweepJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, blobSweepJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET next_blob_sweep_at = %[1]d WHERE name = 'abcde';
			UPDATE accounts SET next_blob_sweep_at = %[1]d WHERE name = 'test1';
			DELETE FROM blobs WHERE id = 1 AND account_name = 'abcde' AND digest = '%[2]s';
			DELETE FROM blobs WHERE id = 2 AND account_name = 'abcde' AND digest = '%[3]s';
			DELETE FROM blobs WHERE id = 3 AND account_name = 'abcde' AND digest = '%[4]s';
			DELETE FROM blobs WHERE id = 4 AND account_name = 'abcde' AND digest = '%[5]s';
			DELETE FROM blobs WHERE id = 5 AND account_name = 'abcde' AND digest = '%[6]s';
			DELETE FROM blobs WHERE id = 6 AND account_name = 'abcde' AND digest = '%[7]s';
		`,
		s.Clock.Now().Add(60*time.Minute).Unix(),
		image.Layers[0].Digest.String(),
		image.Layers[1].Digest.String(),
		image.Config.Digest.String(),
		images[0].Config.Digest.String(),
		images[1].Layers[0].Digest.String(),
		images[1].Config.Digest.String(),
	)

	// now account deletion can go through
	s.Clock.StepBy(1 * time.Minute)
	assert.ErrEqual(t, deleteAccountsJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, deleteAccountsJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().AssertEqualf(`DELETE FROM accounts WHERE name = 'abcde';`)
}

func TestAccountManagementStorageSweep(t *testing.T) {
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)

	tr, tr0 := easypg.NewTracker(t, s.DB.Db)
	tr0.Ignore()
	deleteAccountsJob := j.DeleteAccountsJob(s.Registry)
	managedAccountsJob := j.EnforceManagedAccountsJob(s.Registry)
	sweepStorageJob := j.StorageSweepJob(s.Registry)

	// configure an account to create
	s.AMD.ConfigPath = "../drivers/basic/fixtures/account_management.json"
	s.Clock.StepBy(1 * time.Hour)

	// one account defined which should be created
	assert.ErrEqual(t, managedAccountsJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, managedAccountsJob.ProcessOne(s.Ctx), sql.ErrNoRows)

	// create some unknown blobs ...
	account := models.ReducedAccount{Name: "abcde"}
	testBlob := test.GenerateExampleLayer(33)
	storageID := testBlob.Digest.Encoded()
	sizeBytes := uint64(len(testBlob.Contents))
	must.SucceedT(t, s.SD.AppendToBlob(s.Ctx, account, storageID, 1, Some(sizeBytes), bytes.NewReader(testBlob.Contents)))

	// ... manifests ...
	images := make([]test.Image, 3)
	testImageList1 := test.GenerateImageList(images[0])
	testImageList2 := test.GenerateImageList(images[1])
	for _, manifest := range []test.Bytes{testImageList1.Manifest, testImageList2.Manifest} {
		must.SucceedT(t, s.SD.WriteManifest(s.Ctx, account, "foo", manifest.Digest, bytes.NewReader(manifest.Contents)))
	}

	// ... trivy reports ...
	for idx := range images {
		images[idx] = test.GenerateImage(test.GenerateExampleLayer(int64(idx + 2)))
		images[idx].MustUpload(t, s, fooRepoRef, "")
	}
	imageList := test.GenerateImageList(images[0], images[1])
	manifestList := imageList.MustUpload(t, s, fooRepoRef, "")
	mustUploadDummyTrivyReport(t, s, manifestList)

	tr.DBChanges().Ignore()
	assert.ErrEqual(t, sweepStorageJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, sweepStorageJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, sweepStorageJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET next_storage_sweep_at = %[1]d WHERE name = 'abcde';
			UPDATE accounts SET next_storage_sweep_at = %[1]d WHERE name = 'test1';
			INSERT INTO unknown_blobs (account_name, storage_id, can_be_deleted_at) VALUES ('abcde', '%[3]s', %[2]d);
			INSERT INTO unknown_manifests (account_name, repo_name, digest, can_be_deleted_at) VALUES ('abcde', 'foo', '%[4]s', %[2]d);
			INSERT INTO unknown_trivy_reports (account_name, repo_name, digest, format, can_be_deleted_at) VALUES ('test1', 'foo', '%[5]s', 'json', %[2]d);
		`,
		s.Clock.Now().Add(6*time.Hour).Unix(), // next_storage_sweep_at
		s.Clock.Now().Add(4*time.Hour).Unix(), // can_be_deleted_at
		storageID, testImageList1.Manifest.Digest, manifestList.Digest,
	)

	// .. and some garbage in the storage driver
	testImageList3 := test.GenerateImageList(images[2])
	must.SucceedT(t, s.SD.WriteManifest(s.Ctx, account, "foo", testImageList3.Manifest.Digest, bytes.NewReader(testImageList3.Manifest.Contents)))
	must.SucceedT(t, s.SD.WriteTrivyReport(s.Ctx, account, "foo", testImageList1.Manifest.Digest, trivy.ReportPayload{
		Contents: io.NopCloser(strings.NewReader(`{"report": "test"}`)),
		Format:   "json",
	}))

	// and delete the account again
	s.AMD.ConfigPath = "./fixtures/account_management_empty.json"
	s.Clock.StepBy(2 * time.Hour)
	assert.ErrEqual(t, managedAccountsJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, managedAccountsJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET next_enforcement_at = %d, is_deleting = TRUE, next_deletion_attempt_at = %d WHERE name = 'abcde';
		`,
		s.Clock.Now().Add(1*time.Hour).Unix(),
		s.Clock.Now().Unix(),
	)

	// and we can immeadetaly delete it including all unknown things
	s.Clock.StepBy(1 * time.Hour)
	assert.ErrEqual(t, deleteAccountsJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, deleteAccountsJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	tr.DBChanges().AssertEqualf(`
		DELETE FROM accounts WHERE name = 'abcde';
		DELETE FROM unknown_blobs WHERE account_name = 'abcde' AND storage_id = '%[1]s';
		DELETE FROM unknown_manifests WHERE account_name = 'abcde' AND repo_name = 'foo' AND digest = '%[2]s';
	`, storageID, testImageList1.Manifest.Digest)
}
