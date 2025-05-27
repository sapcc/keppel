// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"github.com/sapcc/keppel/internal/models"
)

// Account represents an account in the API.
type Account struct {
	Name              models.AccountName    `json:"name"`
	AuthTenantID      string                `json:"auth_tenant_id"`
	GCPolicies        []GCPolicy            `json:"gc_policies,omitempty"`
	RBACPolicies      []RBACPolicy          `json:"rbac_policies"`
	ReplicationPolicy *ReplicationPolicy    `json:"replication,omitempty"`
	State             string                `json:"state,omitempty"`
	TagPolicies       []TagPolicy           `json:"tag_policies,omitempty"`
	ValidationPolicy  *ValidationPolicy     `json:"validation,omitempty"`
	PlatformFilter    models.PlatformFilter `json:"platform_filter,omitempty"`
	Metadata          *map[string]string    `json:"metadata"`

	// NOTE: When changing fields, please also adjust type Account in `internal/drivers/basic` as necessary.
}

// RenderAccount converts an account model from the DB into the API representation.
func RenderAccount(dbAccount models.Account) (Account, error) {
	gcPolicies, err := ParseGCPolicies(dbAccount)
	if err != nil {
		return Account{}, err
	}

	rbacPolicies, err := ParseRBACPolicies(dbAccount)
	if err != nil {
		return Account{}, err
	}
	if rbacPolicies == nil {
		// do not render "null" in this field
		rbacPolicies = []RBACPolicy{}
	}

	tagPolicies, err := ParseTagPolicies(dbAccount.TagPoliciesJSON)
	if err != nil {
		return Account{}, err
	}
	if tagPolicies == nil {
		// do not render "null" in this field
		tagPolicies = []TagPolicy{}
	}

	var state string
	if dbAccount.IsDeleting {
		state = "deleting"
	}

	return Account{
		Name:              dbAccount.Name,
		AuthTenantID:      dbAccount.AuthTenantID,
		GCPolicies:        gcPolicies,
		State:             state,
		RBACPolicies:      rbacPolicies,
		ReplicationPolicy: RenderReplicationPolicy(dbAccount),
		TagPolicies:       tagPolicies,
		ValidationPolicy:  RenderValidationPolicy(dbAccount.Reduced()),
		PlatformFilter:    dbAccount.PlatformFilter,
	}, nil
}
