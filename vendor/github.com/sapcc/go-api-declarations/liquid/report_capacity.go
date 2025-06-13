// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package liquid

import . "github.com/majewsky/gg/option"

// ServiceCapacityRequest is the request payload format for POST /v1/report-capacity.
type ServiceCapacityRequest struct {
	// All AZs known to Limes.
	// Many liquids need this information to ensure that:
	//   - AZ-aware capacity is reported for all known AZs, and
	//   - capacity belonging to an invalid AZ is grouped into AvailabilityZoneUnknown.
	// Limes provides this list here to reduce the number of places where this information needs to be maintained manually.
	AllAZs []AvailabilityZone `json:"allAZs"`

	// Must contain an entry for each resource that was declared in type ServiceInfo with "NeedsResourceDemand = true".
	DemandByResource map[ResourceName]ResourceDemand `json:"demandByResource"`
}

// ResourceDemand contains demand statistics for a resource.
// It appears in type ServiceCapacityRequest.
//
// This is used when a liquid needs to be able to reshuffle capacity between different resources based on actual user demand.
type ResourceDemand struct {
	// Demand values are provided in terms of effective capacity.
	// This factor can be applied to them in reverse to obtain values in terms of raw capacity.
	OvercommitFactor OvercommitFactor `json:"overcommitFactor,omitempty"`

	// The actual demand values are AZ-aware.
	// The keys that can be expected in this map depend on the chosen Topology.
	PerAZ map[AvailabilityZone]ResourceDemandInAZ `json:"perAZ"`
}

// ResourceDemandInAZ contains demand statistics for a resource in a single AZ.
// It appears in type ResourceDemand.
//
// The fields are ordered in descending priority.
// All values are in terms of effective capacity, and are sums over all OpenStack projects.
type ResourceDemandInAZ struct {
	// Usage counts all existing usage.
	Usage uint64 `json:"usage"`

	// UnusedCommitments counts all commitments that are confirmed but not covered by existing usage.
	UnusedCommitments uint64 `json:"unusedCommitments"`

	// PendingCommitments counts all commitments that should be confirmed by now, but are not.
	PendingCommitments uint64 `json:"pendingCommitments"`
}

// ServiceCapacityReport is the response payload format for POST /v1/report-capacity.
type ServiceCapacityReport struct {
	// The same version number that is reported in the Version field of a GET /v1/info response.
	// This is used to signal to Limes to refetch GET /v1/info after configuration changes.
	InfoVersion int64 `json:"infoVersion"`

	// Must contain an entry for each resource that was declared in type ServiceInfo with "HasCapacity = true".
	Resources map[ResourceName]*ResourceCapacityReport `json:"resources,omitempty"`

	// Must contain an entry for each metric family that was declared for capacity metrics in type ServiceInfo.
	Metrics map[MetricName][]Metric `json:"metrics,omitempty"`
}

// ResourceCapacityReport contains capacity data for a resource.
// It appears in type ServiceCapacityReport.
type ResourceCapacityReport struct {
	// The keys that are allowed in this map depend on the chosen Topology.
	// See documentation on Topology enum variants for details.
	PerAZ map[AvailabilityZone]*AZResourceCapacityReport `json:"perAZ"`
}

// AZResourceCapacityReport contains capacity data for a resource in a single AZ.
// It appears in type ResourceCapacityReport.
type AZResourceCapacityReport struct {
	// How much capacity is available to Limes in this resource and AZ.
	//
	// Caution: In some cases, underlying capacity can be used by multiple
	// resources. For example, the storage capacity in Manila pools can be used
	// by both the `share_capacity` and `snapshot_capacity` resources. In this case,
	// it is *incorrect* to just report the entire storage capacity in both resources.
	// Limes assumes that whatever number you provide here is free to be
	// allocated exclusively for the respective resource. If physical capacity
	// can be used by multiple resources, you need to split the capacity and
	// report only a chunk of the real capacity in each resource.
	//
	// If you need to split physical capacity between multiple resources like
	// this, the recommended way is to set "NeedsResourceDemand = true" and
	// then split capacity based on the demand reported by Limes.
	Capacity uint64 `json:"capacity"`

	// How much of the Capacity is used, or null if no usage data is available.
	//
	// This should only be reported if the service has an efficient way to obtain this number from the backend.
	// If you can only fill this by summing up usage across all projects, don't; Limes can already do that.
	// This is intended for consistency checks and to estimate how much usage cannot be attributed to OpenStack projects.
	// For example, for compute, this would allow estimating how many VMs are not managed by Nova.
	Usage Option[uint64] `json:"usage,omitzero"`

	// Only filled if the resource is able to report subcapacities in a useful way.
	Subcapacities []Subcapacity `json:"subcapacities,omitempty"`
}
