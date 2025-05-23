// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package basic

import (
	"testing"
	"time"

	"github.com/sapcc/go-bits/assert"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

func TestConfigureAccount(t *testing.T) {
	driver := AccountManagementDriver{
		ConfigPath: "./fixtures/account_management.json",
	}

	listOfAccounts, err := driver.ManagedAccountNames()
	if err != nil {
		t.Fatal(err.Error())
	}
	assert.DeepEqual(t, "account", listOfAccounts, []models.AccountName{"abcde"})

	newAccount, newSecurityScanPolicy, err := driver.ConfigureAccount("abcde")
	if err != nil {
		t.Fatal(err.Error())
	}

	expectedAccount := &keppel.Account{
		Name:         "abcde",
		AuthTenantID: "12345",
		GCPolicies: []keppel.GCPolicy{
			{
				Action: "protect",
				PolicyMatchRule: keppel.PolicyMatchRule{
					NegativeRepositoryRx: "archive/.*",
					RepositoryRx:         ".*/database",
				},
				TimeConstraint: &keppel.GCTimeConstraint{
					FieldName: "pushed_at",
					MaxAge:    keppel.Duration(6 * time.Hour),
				},
			},
			{
				Action:       "delete",
				OnlyUntagged: true,
				PolicyMatchRule: keppel.PolicyMatchRule{
					RepositoryRx: ".*",
				},
			},
		},
		RBACPolicies: []keppel.RBACPolicy{
			{
				Permissions:       []keppel.RBACPermission{"anonymous_pull"},
				RepositoryPattern: "library/.*",
			},
			{
				Permissions:       []keppel.RBACPermission{"pull", "push"},
				RepositoryPattern: "library/alpine",
				UserNamePattern:   ".*@tenant2",
			},
		},
		ReplicationPolicy: &keppel.ReplicationPolicy{
			Strategy: "from_external_on_first_use",
			ExternalPeer: keppel.ReplicationExternalPeerSpec{
				URL: "registry-tertiary.example.org",
			},
		},
		ValidationPolicy: &keppel.ValidationPolicy{
			RequiredLabels: []string{"important-label", "some-label"},
		},
	}

	expectedSecurityScanPolicy := []keppel.SecurityScanPolicy{{
		RepositoryRx:      ".*",
		VulnerabilityIDRx: ".*",
		ExceptFixReleased: true,
		Action: keppel.SecurityScanPolicyAction{
			Assessment: "risk accepted: vulnerabilities without an available fix are not actionable",
			Ignore:     true,
		},
	}}

	assert.DeepEqual(t, "securityScanPolicy", newSecurityScanPolicy, expectedSecurityScanPolicy)
	// we cannot compare with the different pointers, so compare them directly and copy them over
	assert.DeepEqual(t, "account.ReplicationPolicy[0].TimeConstraint", newAccount.GCPolicies[0].TimeConstraint, expectedAccount.GCPolicies[0].TimeConstraint)
	expectedAccount.GCPolicies[0].TimeConstraint = newAccount.GCPolicies[0].TimeConstraint
	assert.DeepEqual(t, "account.ReplicationPolicy", *newAccount.ReplicationPolicy, *expectedAccount.ReplicationPolicy)
	expectedAccount.ReplicationPolicy = newAccount.ReplicationPolicy
	assert.DeepEqual(t, "account.ValidationPolicy", *newAccount.ValidationPolicy, *expectedAccount.ValidationPolicy)
	expectedAccount.ValidationPolicy = newAccount.ValidationPolicy

	assert.DeepEqual(t, "account", newAccount, expectedAccount)
}
