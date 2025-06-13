// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package liquid

import . "github.com/majewsky/gg/option"

// ServiceQuotaRequest is the request payload format for PUT /v1/projects/:uuid/quota.
type ServiceQuotaRequest struct {
	Resources map[ResourceName]ResourceQuotaRequest `json:"resources"`

	// Metadata about the project from Keystone.
	// Only included if the ServiceInfo declared a need for it.
	ProjectMetadata Option[ProjectMetadata] `json:"projectMetadata,omitzero"`
}

// ResourceQuotaRequest contains new quotas for a single resource.
// It appears in type ServiceQuotaRequest.
type ResourceQuotaRequest struct {
	// For FlatTopology and AZAwareTopology, this is the only field that is filled, and PerAZ will be nil.
	// For AZSeparatedTopology, this contains the sum of the quotas across all AZs (for compatibility purposes).
	Quota uint64 `json:"quota"`

	// PerAZ will only be filled for AZSeparatedTopology.
	PerAZ map[AvailabilityZone]AZResourceQuotaRequest `json:"perAZ,omitempty"`
}

// AZResourceQuotaRequest contains the new quota value for a single resource and AZ.
// It appears in type ResourceQuotaRequest.
type AZResourceQuotaRequest struct {
	Quota uint64 `json:"quota"`

	// This struct looks superfluous (why not just have a bare uint64?), but in
	// the event that more data needs to be added in the future, having this
	// struct allows for that to be a backwards-compatible change.
}
