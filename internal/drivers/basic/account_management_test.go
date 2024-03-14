/******************************************************************************
*
*  Copyright 2024 SAP SE
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

package basic

import (
	"testing"

	"github.com/sapcc/keppel/internal/keppel"

	"github.com/sapcc/go-bits/assert"
)

func TestAccountManagementDriver(t *testing.T) {
	driver := AccountManagementDriver{
		configPath: "./fixtures/account_management.yaml",
	}

	listOfAccounts, err := driver.ManagedAccountNames()
	if err != nil {
		t.Fatalf(err.Error())
	}
	assert.DeepEqual(t, "account", listOfAccounts, []string{"abcde"})

	account := keppel.Account{
		IsManaged:    true,
		Name:         "abcde",
		AuthTenantID: "1245",
	}
	newAccount, err := driver.ConfigureAccount(nil, account)
	if err != nil {
		t.Fatalf(err.Error())
	}

	expectedAccount := keppel.Account{
		Name:                     "abcde",
		AuthTenantID:             "1245",
		ExternalPeerURL:          "registry-tertiary.example.org",
		RequiredLabels:           "important-label,some-label",
		IsManaged:                true,
		RBACPoliciesJSON:         "[{\"match_repository\":\"library/.*\",\"permissions\":[\"anonymous_pull\"]},{\"match_repository\":\"library/alpine\",\"match_username\":\".*@tenant2\",\"permissions\":[\"pull\",\"push\"]}]",
		GCPoliciesJSON:           "[{\"match_repository\":\".*/database\",\"except_repository\":\"archive/.*\",\"time_constraint\":{\"on\":\"pushed_at\",\"newer_than\":{\"value\":6,\"unit\":\"h\"}},\"action\":\"protect\"},{\"match_repository\":\".*\",\"only_untagged\":true,\"action\":\"delete\"}]",
		SecurityScanPoliciesJSON: "[{\"match_repository\":\".*\",\"match_vulnerability_id\":\".*\",\"except_fix_released\":true,\"action\":{\"assessment\":\"risk accepted: vulnerabilities without an available fix are not actionable\",\"ignore\":true}}]",
	}
	assert.DeepEqual(t, "account", newAccount, expectedAccount)
}
