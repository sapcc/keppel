/*******************************************************************************
*
* Copyright 2024 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package models

import (
	"strings"
	"time"
)

// AccountName identifies an account. This typedef is used to distinguish these
// names from other string values.
type AccountName string

// Account contains a record from the `accounts` table.
type Account struct {
	Name         AccountName `db:"name"`
	AuthTenantID string      `db:"auth_tenant_id"`

	// UpstreamPeerHostName is set if and only if the "on_first_use" replication strategy is used.
	UpstreamPeerHostName string `db:"upstream_peer_hostname"`
	// ExternalPeerURL, ExternalPeerUserName and ExternalPeerPassword are set if
	// and only if the "from_external_on_first_use" replication strategy is used.
	ExternalPeerURL      string `db:"external_peer_url"`
	ExternalPeerUserName string `db:"external_peer_username"`
	ExternalPeerPassword string `db:"external_peer_password"`
	// PlatformFilter restricts which submanifests get replicated when a list manifest is replicated.
	PlatformFilter PlatformFilter `db:"platform_filter"`

	// RequiredLabels is a comma-separated list of labels that must be present on
	// all image manifests in this account.
	RequiredLabels string `db:"required_labels"`
	// IsDeleting indicates whether the account is currently being deleted.
	IsDeleting bool `db:"is_deleting"`
	// IsManaged indicates if the account was created by AccountManagementDriver
	IsManaged bool `db:"is_managed"`

	// RBACPoliciesJSON contains a JSON string of []keppel.RBACPolicy, or the empty string.
	RBACPoliciesJSON string `db:"rbac_policies_json"`
	// GCPoliciesJSON contains a JSON string of []keppel.GCPolicy, or the empty string.
	GCPoliciesJSON string `db:"gc_policies_json"`
	// SecurityScanPoliciesJSON contains a JSON string of []keppel.SecurityScanPolicy, or the empty string.
	SecurityScanPoliciesJSON string `db:"security_scan_policies_json"`

	NextBlobSweepedAt            *time.Time `db:"next_blob_sweep_at"`              // see tasks.BlobSweepJob
	NextDeletionAttempt          *time.Time `db:"next_deletion_attempt_at"`        // see tasks.AccountDeletionJob
	NextEnforcementAt            *time.Time `db:"next_enforcement_at"`             // see tasks.CreateManagedAccountsJob
	NextStorageSweepedAt         *time.Time `db:"next_storage_sweep_at"`           // see tasks.StorageSweepJob
	NextFederationAnnouncementAt *time.Time `db:"next_federation_announcement_at"` // see tasks.AnnounceAccountToFederationJob

	// TODO: remove once the Elektra UI has been updated to not require this flag to proceed with account deletion
	InMaintenance bool `db:"in_maintenance"`
}

// Reduced converts an Account into a ReducedAccount.
func (a Account) Reduced() ReducedAccount {
	return ReducedAccount{
		Name:                 a.Name,
		AuthTenantID:         a.AuthTenantID,
		UpstreamPeerHostName: a.UpstreamPeerHostName,
		ExternalPeerURL:      a.ExternalPeerURL,
		ExternalPeerUserName: a.ExternalPeerUserName,
		ExternalPeerPassword: a.ExternalPeerPassword,
		PlatformFilter:       a.PlatformFilter,
		RequiredLabels:       a.RequiredLabels,
		IsDeleting:           a.IsDeleting,
	}
}

// ReducedAccount contains just the fields from type Account that the Registry API is most interested in.
// This type exists to avoid loading the large payload fields in type Account when we don't need to,
// which is a significant memory optimization for the keppel-api process.
type ReducedAccount struct {
	Name         AccountName
	AuthTenantID string

	// replication policy
	UpstreamPeerHostName string
	ExternalPeerURL      string
	ExternalPeerUserName string
	ExternalPeerPassword string
	PlatformFilter       PlatformFilter

	// validation policy, status
	RequiredLabels string
	IsDeleting     bool

	// NOTE: When adding or removing fields, always adjust Account.Reduced() and keppel.FindReducedAccount() too!
}

// SplitRequiredLabels parses the RequiredLabels field.
func (a ReducedAccount) SplitRequiredLabels() []string {
	return strings.Split(a.RequiredLabels, ",")
}
