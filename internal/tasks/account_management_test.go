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
	job := j.EnforceManagedAccountsJob(s.Registry)

	// we haven't configured any account, so it should do nothing
	s.AMD.ConfigPath = "./fixtures/account_management_empty.json"
	expectError(t, sql.ErrNoRows.Error(), job.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEmpty()

	// configure an account to create
	s.AMD.ConfigPath = "../drivers/basic/fixtures/account_management.json"
	s.Clock.StepBy(1 * time.Hour)

	// we only have one account defined which should be created
	expectSuccess(t, job.ProcessOne(s.Ctx))
	// since we are enforcing that account, no error is returned
	expectError(t, sql.ErrNoRows.Error(), job.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEqualf(`
		INSERT INTO accounts (name, auth_tenant_id, required_labels, external_peer_url, gc_policies_json, security_scan_policies_json, rbac_policies_json, is_managed, next_enforcement_at) VALUES ('abcde', '12345', 'important-label,some-label', 'registry-tertiary.example.org', '[{"match_repository":".*/database","except_repository":"archive/.*","time_constraint":{"on":"pushed_at","newer_than":{"value":6,"unit":"h"}},"action":"protect"},{"match_repository":".*","only_untagged":true,"action":"delete"}]', '[{"match_repository":".*","match_vulnerability_id":".*","except_fix_released":true,"action":{"assessment":"risk accepted: vulnerabilities without an available fix are not actionable","ignore":true}}]', '[{"match_repository":"library/.*","permissions":["anonymous_pull"]},{"match_repository":"library/alpine","match_username":".*@tenant2","permissions":["pull","push"]}]', TRUE, %d);
		`,
		s.Clock.Now().Add(1*time.Hour).Unix())

	// test if errors are propagated
	s.AMD.ConfigPath = "./fixtures/account_management_error.json"
	s.Clock.StepBy(2 * time.Hour)
	expectError(t, fmt.Sprintf("could not configure managed account %q: %s", "", processor.ErrAccountNameEmpty.Error()), job.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEmpty()

	// and delete the account again
	s.AMD.ConfigPath = "./fixtures/account_management_empty.json"
	s.Clock.StepBy(2 * time.Hour)
	expectSuccess(t, job.ProcessOne(s.Ctx))
	expectError(t, sql.ErrNoRows.Error(), job.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEqualf(`
		DELETE FROM accounts WHERE name = 'abcde';
	`)
}

func TestAccountManagementWithComplexDeletion(t *testing.T) {
	j, s := setup(t)
	job := j.EnforceManagedAccountsJob(s.Registry)

	tr, tr0 := easypg.NewTracker(t, s.DB.DbMap.Db)
	tr0.Ignore()

	// create a managed account named "abcde"
	s.AMD.ConfigPath = "./fixtures/account_management_basic.json"
	expectSuccess(t, job.ProcessOne(s.Ctx))
	expectError(t, sql.ErrNoRows.Error(), job.ProcessOne(s.Ctx))
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

	// try to delete the managed account: first attempt will set in_maintenance and delete the image, but nothing more
	s.AMD.ConfigPath = "./fixtures/account_management_empty.json"
	s.Clock.StepBy(2 * time.Hour)
	expectSuccess(t, job.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEqualf(`
		UPDATE accounts SET in_maintenance = TRUE, next_enforcement_at = %[1]d WHERE name = 'abcde';
		DELETE FROM manifest_blob_refs WHERE repo_id = 2 AND digest = '%[2]s' AND blob_id = 1;
		DELETE FROM manifest_blob_refs WHERE repo_id = 2 AND digest = '%[2]s' AND blob_id = 2;
		DELETE FROM manifest_blob_refs WHERE repo_id = 2 AND digest = '%[2]s' AND blob_id = 3;
		DELETE FROM manifest_contents WHERE repo_id = 2 AND digest = '%[2]s';
		DELETE FROM manifests WHERE repo_id = 2 AND digest = '%[2]s';
		DELETE FROM tags WHERE repo_id = 2 AND name = 'latest';
		DELETE FROM trivy_security_info WHERE repo_id = 2 AND digest = '%[2]s';
		`,
		s.Clock.Now().Add(1*time.Minute).Unix(),
		image.Manifest.Digest.String(),
	)

	// second try: since there are no manifests left...
	// - all repos (and thus blob mounts) will be deleted
	// - all blobs are marked for immediate deletion, and a blob GC is scheduled
	s.Clock.StepBy(2 * time.Minute)
	expectSuccess(t, job.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEqualf(`
		UPDATE accounts SET next_blob_sweep_at = %[1]d, next_enforcement_at = %[2]d WHERE name = 'abcde';
		DELETE FROM blob_mounts WHERE blob_id = 1 AND repo_id = 2;
		DELETE FROM blob_mounts WHERE blob_id = 2 AND repo_id = 2;
		DELETE FROM blob_mounts WHERE blob_id = 3 AND repo_id = 2;
		UPDATE blobs SET can_be_deleted_at = %[1]d WHERE id = 1 AND account_name = 'abcde' AND digest = '%[3]s';
		UPDATE blobs SET can_be_deleted_at = %[1]d WHERE id = 2 AND account_name = 'abcde' AND digest = '%[4]s';
		UPDATE blobs SET can_be_deleted_at = %[1]d WHERE id = 3 AND account_name = 'abcde' AND digest = '%[5]s';
		DELETE FROM repos WHERE id = 2 AND account_name = 'abcde' AND name = 'foo';
		`,
		s.Clock.Now().Unix(),
		s.Clock.Now().Add(1*time.Minute).Unix(),
		image.Layers[0].Digest.String(),
		image.Layers[1].Digest.String(),
		image.Config.Digest.String(),
	)

	// to make further progress, the scheduled blob GC needs to go through first
	// (we need to run this twice because the common test setup includes another account that is irrelevant to this test)
	s.Clock.StepBy(1 * time.Second)
	blobGCJob := j.BlobSweepJob(s.Registry)
	expectSuccess(t, blobGCJob.ProcessOne(s.Ctx))
	expectSuccess(t, blobGCJob.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEqualf(`
		UPDATE accounts SET next_blob_sweep_at = %[1]d WHERE name = 'abcde';
		UPDATE accounts SET next_blob_sweep_at = %[1]d WHERE name = 'test1';
		DELETE FROM blobs WHERE id = 1 AND account_name = 'abcde' AND digest = '%[2]s';
		DELETE FROM blobs WHERE id = 2 AND account_name = 'abcde' AND digest = '%[3]s';
		DELETE FROM blobs WHERE id = 3 AND account_name = 'abcde' AND digest = '%[4]s';
		`,
		s.Clock.Now().Add(1*time.Hour).Unix(),
		image.Layers[0].Digest.String(),
		image.Layers[1].Digest.String(),
		image.Config.Digest.String(),
	)

	// third try: this time, the account deletion can go through
	s.Clock.StepBy(2 * time.Minute)
	expectSuccess(t, job.ProcessOne(s.Ctx))
	tr.DBChanges().AssertEqualf(`
		DELETE FROM accounts WHERE name = 'abcde';
	`)
}
