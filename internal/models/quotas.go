// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package models

// Quotas contains a record from the `quotas` table.
//
// The JSON serialization is used in audit events for quota changes.
type Quotas struct {
	AuthTenantID  string `db:"auth_tenant_id" json:"-"`
	Bytes         uint64 `db:"bytes" json:"bytes,omitempty"`
	ManifestCount uint64 `db:"manifests" json:"manifests"`
}
