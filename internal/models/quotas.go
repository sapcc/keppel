// SPDX-FileCopyrightText: 2024 SAP SE
// SPDX-License-Identifier: Apache-2.0

package models

// Quotas contains a record from the `quotas` table.
//
// The JSON serialization is used in audit events for quota changes.
type Quotas struct {
	AuthTenantID  string `db:"auth_tenant_id" json:"-"`
	ManifestCount uint64 `db:"manifests" json:"manifests"`
}

// DefaultQuotas creates a new Quotas instance with the default quotas.
func DefaultQuotas(authTenantID string) *Quotas {
	// Right now, the default quota is always 0. The value of having this function
	// is to ensure that we only need to change this place if this ever changes.
	return &Quotas{
		AuthTenantID:  authTenantID,
		ManifestCount: 0,
	}
}
