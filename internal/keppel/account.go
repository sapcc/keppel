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
	ValidationPolicy  *ValidationPolicy     `json:"validation,omitempty"`
	PlatformFilter    models.PlatformFilter `json:"platform_filter,omitempty"`
	Metadata          *map[string]string    `json:"metadata"`
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
		ValidationPolicy:  RenderValidationPolicy(dbAccount.Reduced()),
		PlatformFilter:    dbAccount.PlatformFilter,
	}, nil
}
