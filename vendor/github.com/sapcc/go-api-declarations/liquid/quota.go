// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package liquid

import (
	. "github.com/majewsky/gg/option"

	"github.com/sapcc/go-api-declarations/internal/clone"
)

// ServiceQuotaRequest is the request payload format for PUT /v1/projects/:uuid/quota.
type ServiceQuotaRequest struct {
	Resources map[ResourceName]ResourceQuotaRequest `json:"resources"`

	// Metadata about the project from Keystone.
	// Only included if the ServiceInfo declared a need for it.
	ProjectMetadata Option[ProjectMetadata] `json:"projectMetadata,omitzero"`
}

// Clone returns a deep copy of the given ServiceQuotaRequest.
func (i ServiceQuotaRequest) Clone() ServiceQuotaRequest {
	cloned := i
	cloned.Resources = clone.MapRecursively(i.Resources)
	return cloned
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

// Clone returns a deep copy of the given ResourceQuotaRequest.
func (i ResourceQuotaRequest) Clone() ResourceQuotaRequest {
	cloned := i
	cloned.PerAZ = clone.MapRecursively(i.PerAZ)
	return cloned
}

// AZResourceQuotaRequest contains the new quota value for a single resource and AZ.
// It appears in type ResourceQuotaRequest.
type AZResourceQuotaRequest struct {
	Quota uint64 `json:"quota"`

	// This struct looks superfluous (why not just have a bare uint64?), but in
	// the event that more data needs to be added in the future, having this
	// struct allows for that to be a backwards-compatible change.
}

// Clone returns a deep copy of the given AZResourceQuotaRequest.
func (i AZResourceQuotaRequest) Clone() AZResourceQuotaRequest {
	// this method is only offered for compatibility with future expansion;
	// right now, all fields are copied by-value automatically
	return i
}
