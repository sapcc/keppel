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
	"testing"
	"time"

	"github.com/sapcc/go-bits/easypg"

	"github.com/sapcc/keppel/internal/processor"
)

func TestAccountManagementDriver(t *testing.T) {
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)

	tr, _ := easypg.NewTracker(t, s.DB.DbMap.Db)
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
		INSERT INTO accounts (name, auth_tenant_id, required_labels, next_federation_announcement_at, external_peer_url, gc_policies_json, security_scan_policies_json, rbac_policies_json, is_managed, next_account_enforcement_at) VALUES ('abcde', '1245', 'important-label,some-label', 10800, 'registry-tertiary.example.org', '[{"match_repository":".*/database","except_repository":"archive/.*","time_constraint":{"on":"pushed_at","newer_than":{"value":6,"unit":"h"}},"action":"protect"},{"match_repository":".*","only_untagged":true,"action":"delete"}]', '[{"match_repository":".*","match_vulnerability_id":".*","except_fix_released":true,"action":{"assessment":"risk accepted: vulnerabilities without an available fix are not actionable","ignore":true}}]', '[{"match_repository":"library/.*","permissions":["anonymous_pull"]},{"match_repository":"library/alpine","match_username":".*@tenant2","permissions":["pull","push"]}]', TRUE, 10800);
	`)

	// test if errors are propagated
	s.AMD.ConfigPath = "./fixtures/account_management_error.json"
	s.Clock.StepBy(2 * time.Hour)
	expectError(t, processor.ErrAccountNameEmpty.Error(), job.ProcessOne(s.Ctx))
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
