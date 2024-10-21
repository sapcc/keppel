/******************************************************************************
*
*  Copyright 2023 SAP SE
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
	"testing"
	"time"

	"github.com/sapcc/go-bits/easypg"

	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/processor"
	"github.com/sapcc/keppel/internal/test"
)

func TestAccountManagementBasic(t *testing.T) {
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)

	tr, tr0 := easypg.NewTracker(t, s.DB.DbMap.Db)
	tr0.Ignore()
	managedAccountsJob := j.EnforceManagedAccountsJob(s.Registry)
	deleteAccountsJob := j.DeleteAccountsJob(s.Registry)

	// we haven't configured any account, so it should do nothing
	s.AMD.ConfigPath = "./fixtures/account_management_empty.json"
	expectError(t, sql.ErrNoRows.Error(), managedAccountsJob.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEmpty()

	// configure an account to create
	s.AMD.ConfigPath = "../drivers/basic/fixtures/account_management.json"
	s.Clock.StepBy(1 * time.Hour)

	// we only have one account defined which should be created
	expectSuccess(t, managedAccountsJob.ProcessOne(s.Ctx))
	// since we are enforcing that account, no error is returned
	expectError(t, sql.ErrNoRows.Error(), managedAccountsJob.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEqualf(`
		INSERT INTO accounts (name, auth_tenant_id, required_labels, external_peer_url, gc_policies_json, security_scan_policies_json, rbac_policies_json, is_managed, next_enforcement_at) VALUES ('abcde', '12345', 'important-label,some-label', 'registry-tertiary.example.org', '[{"match_repository":".*/database","except_repository":"archive/.*","time_constraint":{"on":"pushed_at","newer_than":{"value":6,"unit":"h"}},"action":"protect"},{"match_repository":".*","only_untagged":true,"action":"delete"}]', '[{"match_repository":".*","match_vulnerability_id":".*","except_fix_released":true,"action":{"assessment":"risk accepted: vulnerabilities without an available fix are not actionable","ignore":true}}]', '[{"match_repository":"library/.*","permissions":["anonymous_pull"]},{"match_repository":"library/alpine","match_username":".*@tenant2","permissions":["pull","push"]}]', TRUE, %d);
		`,
		s.Clock.Now().Add(1*time.Hour).Unix())

	// test if errors are propagated
	s.AMD.ConfigPath = "./fixtures/account_management_error.json"
	s.Clock.StepBy(2 * time.Hour)
	expectError(t, fmt.Sprintf("could not configure managed account %q: %s", "", processor.ErrAccountNameEmpty.Error()), managedAccountsJob.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEmpty()

	// and delete the account again
	s.AMD.ConfigPath = "./fixtures/account_management_empty.json"
	s.Clock.StepBy(2 * time.Hour)
	// now the account is being marked for deletion
	expectSuccess(t, managedAccountsJob.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET next_enforcement_at = %d, is_deleting = TRUE, next_deletion_attempt_at = %d WHERE name = 'abcde';
		`,
		s.Clock.Now().Add(1*time.Hour).Unix(),
		s.Clock.Now().Unix(),
	)

	// and we can immeadetaly delete it yet because it has no manifests and blobs attached to it
	s.Clock.StepBy(1 * time.Hour)
	expectSuccess(t, deleteAccountsJob.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEqual(`
		DELETE FROM accounts WHERE name = 'abcde';
	`)
}

func TestAccountManagementWithReplicaCreation(t *testing.T) {
	test.WithRoundTripper(func(_ *test.RoundTripper) {
		_, s1 := setup(t)
		j2, s2 := setupReplica(t, s1, "on_first_use")

		tr, tr0 := easypg.NewTracker(t, s2.DB.DbMap.Db)
		tr0.Ignore()

		// The setup already includes an account "test1" set up on both ends, but we
		// want to test the setup of a managed replica account, so we will use a
		// fresh account called "managed" instead.
		mustDo(t, s1.DB.Insert(&models.Account{Name: "managed", AuthTenantID: "managedauthtenant"}))
		s1.FD.NextSubleaseTokenSecretToIssue = "thisisasecret"
		s2.FD.ValidSubleaseTokenSecrets["managed"] = "thisisasecret"

		// test seeding the replica on the secondary side
		s2.AMD.ConfigPath = "./fixtures/account_management_replica.json"
		job := j2.EnforceManagedAccountsJob(s2.Registry)
		expectSuccess(t, job.ProcessOne(s2.Ctx))

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

	tr, tr0 := easypg.NewTracker(t, s.DB.DbMap.Db)
	tr0.Ignore()

	// create a managed account named "abcde"
	s.AMD.ConfigPath = "./fixtures/account_management_basic.json"
	expectSuccess(t, managedAccountsJob.ProcessOne(s.Ctx))
	expectError(t, sql.ErrNoRows.Error(), managedAccountsJob.ProcessOne(s.Ctx))
	tr.DBChanges().Ignore()

	// give quota to its auth tenant
	mustDo(t, s.DB.Insert(&models.Quotas{
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
	tr.DBChanges().Ignore()

	// remove the managed account: this will set is_deleting, but nothing more
	s.AMD.ConfigPath = "./fixtures/account_management_empty.json"
	s.Clock.StepBy(2 * time.Hour)
	expectSuccess(t, managedAccountsJob.ProcessOne(s.Ctx))
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
	expectSuccess(t, deleteAccountsJob.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET next_blob_sweep_at = %[1]d, next_deletion_attempt_at = %[2]d WHERE name = 'abcde';
			DELETE FROM blob_mounts WHERE blob_id = 1 AND repo_id = 2;
			DELETE FROM blob_mounts WHERE blob_id = 2 AND repo_id = 2;
			DELETE FROM blob_mounts WHERE blob_id = 3 AND repo_id = 2;
			UPDATE blobs SET can_be_deleted_at = %[1]d WHERE id = 1 AND account_name = 'abcde' AND digest = '%[4]s';
			UPDATE blobs SET can_be_deleted_at = %[1]d WHERE id = 2 AND account_name = 'abcde' AND digest = '%[5]s';
			UPDATE blobs SET can_be_deleted_at = %[1]d WHERE id = 3 AND account_name = 'abcde' AND digest = '%[6]s';
			DELETE FROM manifest_blob_refs WHERE repo_id = 2 AND digest = '%[3]s' AND blob_id = 1;
			DELETE FROM manifest_blob_refs WHERE repo_id = 2 AND digest = '%[3]s' AND blob_id = 2;
			DELETE FROM manifest_blob_refs WHERE repo_id = 2 AND digest = '%[3]s' AND blob_id = 3;
			DELETE FROM manifest_contents WHERE repo_id = 2 AND digest = '%[3]s';
			DELETE FROM manifests WHERE repo_id = 2 AND digest = '%[3]s';
			DELETE FROM repos WHERE id = 2 AND account_name = 'abcde' AND name = 'foo';
			DELETE FROM tags WHERE repo_id = 2 AND name = 'latest';
			DELETE FROM trivy_security_info WHERE repo_id = 2 AND digest = '%[3]s';
		`,
		s.Clock.Now().Unix(),
		s.Clock.Now().Add(1*time.Minute).Unix(),
		image.Manifest.Digest.String(),
		image.Layers[0].Digest.String(),
		image.Layers[1].Digest.String(),
		image.Config.Digest.String(),
	)

	// TODO: fix the can_be_deleted_at reset
	s.Clock.StepBy(3 * time.Minute)
	expectSuccess(t, deleteAccountsJob.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET next_blob_sweep_at = %[1]d, next_deletion_attempt_at = %[2]d WHERE name = 'abcde';
			UPDATE blobs SET can_be_deleted_at = %[1]d WHERE id = 1 AND account_name = 'abcde' AND digest = '%[3]s';
			UPDATE blobs SET can_be_deleted_at = %[1]d WHERE id = 2 AND account_name = 'abcde' AND digest = '%[4]s';
			UPDATE blobs SET can_be_deleted_at = %[1]d WHERE id = 3 AND account_name = 'abcde' AND digest = '%[5]s';
		`,
		s.Clock.Now().Unix(),
		s.Clock.Now().Add(1*time.Minute).Unix(),
		image.Layers[0].Digest.String(),
		image.Layers[1].Digest.String(),
		image.Config.Digest.String(),
	)

	s.Clock.StepBy(1 * time.Minute)
	// we need to run this twice because the common test setup includes another account that is irrelevant to this test
	expectSuccess(t, blobSweepJob.ProcessOne(s.Ctx))
	expectSuccess(t, blobSweepJob.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEqualf(`
			UPDATE accounts SET next_blob_sweep_at = %[1]d WHERE name = 'abcde';
			UPDATE accounts SET next_blob_sweep_at = %[1]d WHERE name = 'test1';
			DELETE FROM blobs WHERE id = 1 AND account_name = 'abcde' AND digest = '%[2]s';
			DELETE FROM blobs WHERE id = 2 AND account_name = 'abcde' AND digest = '%[3]s';
			DELETE FROM blobs WHERE id = 3 AND account_name = 'abcde' AND digest = '%[4]s';
		`,
		s.Clock.Now().Add(60*time.Minute).Unix(),
		image.Layers[0].Digest.String(),
		image.Layers[1].Digest.String(),
		image.Config.Digest.String(),
	)
}
