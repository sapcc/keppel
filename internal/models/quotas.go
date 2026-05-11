// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package models

import "math"

// Quotas contains a record from the `quotas` table.
//
// The JSON serialization is used in audit events for quota changes.
type Quotas struct {
	AuthTenantID  string `db:"auth_tenant_id" json:"-"`
	Bytes         uint64 `db:"bytes" json:"bytes,omitempty"`
	ManifestCount uint64 `db:"manifests" json:"manifests"`
}

// DefaultQuotas creates a new Quotas instance with the default quotas.
func DefaultQuotas(authTenantID string, trackBytesQuota bool) Quotas {
	quotas := Quotas{
		AuthTenantID:  authTenantID,
		Bytes:         0,
		ManifestCount: 0,
	}

	if !trackBytesQuota {
		quotas.Bytes = math.MaxInt64
	}

	return quotas
}
