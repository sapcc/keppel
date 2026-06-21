// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package basic

import (
	"testing"
	"time"

	"github.com/sapcc/go-bits/must"
	"go.xyrillian.de/gg/assert"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

func TestConfigureAccount(t *testing.T) {
	driver := AccountManagementDriver{
		ConfigPath: "./fixtures/account_management.json",
	}

	listOfAccounts := must.ReturnT(driver.ManagedAccountNames())(t)
	assert.Equal(t, listOfAccounts, []models.AccountName{"abcde"})

	maybeNewAccount, newSecurityScanPolicy, err := driver.ConfigureAccount("abcde")
	must.SucceedT(t, err)
	newAccount, ok := maybeNewAccount.Unpack()
	if !ok {
		t.Fatal("ConfigureAccount returned None[keppel.Account]()")
	}

	assert.Equal(t, newAccount, keppel.Account{
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
			RuleForManifest: "'important-label' in labels && 'some-label' in labels",
			RequiredLabels:  []string{"important-label", "some-label"},
		},
	})

	assert.Equal(t, newSecurityScanPolicy, []keppel.SecurityScanPolicy{{
		RepositoryRx:      ".*",
		VulnerabilityIDRx: ".*",
		ExceptFixReleased: true,
		Action: keppel.SecurityScanPolicyAction{
			Assessment: "risk accepted: vulnerabilities without an available fix are not actionable",
			Ignore:     true,
		},
	}})
}
