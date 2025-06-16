// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package liquid

import (
	"encoding/json"
	"math/big"

	. "github.com/majewsky/gg/option"
)

// ServiceUsageRequest is the request payload format for POST /v1/projects/:uuid/report-usage.
type ServiceUsageRequest struct {
	// All AZs known to Limes.
	// Many liquids need this information to ensure that:
	//   - AZ-aware usage is reported for all known AZs, and
	//   - usage belonging to an invalid AZ is grouped into AvailabilityZoneUnknown.
	// Limes provides this list here to reduce the number of places where this information needs to be maintained manually.
	AllAZs []AvailabilityZone `json:"allAZs"`

	// Metadata about the project from Keystone.
	// Only included if the ServiceInfo declared a need for it.
	ProjectMetadata Option[ProjectMetadata] `json:"projectMetadata,omitzero"`

	// The serialized state from the previous ServiceUsageReport received by Limes for this project, if any.
	// Refer to the same field on type ServiceUsageReport for details.
	SerializedState json.RawMessage `json:"serializedState,omitempty"`
}

// ServiceUsageReport is the response payload format for POST /v1/projects/:uuid/report-usage.
type ServiceUsageReport struct {
	// The same version number that is reported in the Version field of a GET /v1/info response.
	// This is used to signal to Limes to refetch GET /v1/info after configuration changes.
	InfoVersion int64 `json:"infoVersion"`

	// Must contain an entry for each resource that was declared in type ServiceInfo.
	Resources map[ResourceName]*ResourceUsageReport `json:"resources,omitempty"`

	// Must contain an entry for each rate that was declared in type ServiceInfo.
	Rates map[RateName]*RateUsageReport `json:"rates,omitempty"`

	// Must contain an entry for each metric family that was declared for usage metrics in type ServiceInfo.
	Metrics map[MetricName][]Metric `json:"metrics,omitempty"`

	// Opaque state for Limes to persist and return to the liquid in the next ServiceUsageRequest for the same project.
	// This should only be used if the liquid needs to store project-level data, but does not have its own database.
	//
	// This field is intended specifically for rate usage measurements, esp. to detect and handle counter resets in the backend.
	// In this case, it might contain information like "counter C had value V at time T".
	//
	// Warning: As of the time of this writing, Limes may not loop this field back consistently if the liquid has resources.
	// This behavior is considered a bug and will be fixed eventually.
	SerializedState json.RawMessage `json:"serializedState,omitempty"`
}

// ResourceUsageReport contains usage data for a resource in a single project.
// It appears in type ServiceUsageReport.
type ResourceUsageReport struct {
	// If true, this project is forbidden from accessing this resource.
	// This has two consequences:
	//   - If the resource has quota, Limes will never try to assign quota for this resource to this project except to cover existing usage.
	//   - If the project has no usage in this resource, Limes will hide this resource from project reports.
	Forbidden bool `json:"forbidden"`

	// This shall be None if and only if the resource is declared with "HasQuota = false" or with AZSeparatedTopology.
	// A negative value, usually -1, indicates "infinite quota" (i.e., the absence of a quota).
	Quota Option[int64] `json:"quota,omitzero"`

	// The keys that are allowed in this map depend on the chosen Topology.
	// See documentation on Topology enum variants for details.
	//
	// Tip: When filling this by starting from a non-AZ-aware usage number that is later broken down with AZ-aware data, use func PrepareForBreakdownInto.
	PerAZ map[AvailabilityZone]*AZResourceUsageReport `json:"perAZ"`
}

// AZResourceUsageReport contains usage data for a resource in a single project and AZ.
// It appears in type ResourceUsageReport.
type AZResourceUsageReport struct {
	// The amount of usage for this resource.
	Usage uint64 `json:"usage"`

	// The amount of physical usage for this resource.
	// Only reported if this notion makes sense for the particular resource.
	//
	// For example, consider the Manila resource "share_capacity".
	// If a project has 5 shares, each with 10 GiB size and each containing 1 GiB data, then Usage = 50 GiB and PhysicalUsage = 5 GiB.
	// It is not allowed to report 5 GiB as Usage in this situation, since the 50 GiB value is used when judging whether the Quota fits.
	PhysicalUsage Option[uint64] `json:"physicalUsage,omitzero"`

	// This shall be non-null if and only if the resource is declared with AZSeparatedTopology.
	// A negative value, usually -1, indicates "infinite quota" (i.e., the absence of a quota).
	Quota Option[int64] `json:"quota,omitzero"`

	// Only filled if the resource is able to report subresources for this usage in a useful way.
	Subresources []Subresource `json:"subresources,omitempty"`
}

// PrepareForBreakdownInto is a convenience constructor for the PerAZ field of ResourceUsageReport.
// It builds a map with zero-valued entries for all of the named AZs.
// Furthermore, if the provided AZ report contains nonzero usage, it is placed in the AvailabilityZoneUnknown key.
//
// This constructor can be used when the total usage data is reported without AZ awareness.
// An AZ breakdown can later be added with the AddLocalizedUsage() method of ResourceUsageReport.
func (r AZResourceUsageReport) PrepareForBreakdownInto(allAZs []AvailabilityZone) map[AvailabilityZone]*AZResourceUsageReport {
	result := make(map[AvailabilityZone]*AZResourceUsageReport, len(allAZs)+1)
	for _, az := range allAZs {
		var empty AZResourceUsageReport
		result[az] = &empty
	}
	if r.Usage > 0 {
		result[AvailabilityZoneUnknown] = &r
	}
	return result
}

// AddLocalizedUsage subtracts the given `usage from AvailabilityZoneUnknown (if any) and adds it to the given AZ instead.
//
// This is used when breaking down a usage total reported by a non-AZ-aware API by iterating over AZ-localized objects.
// The hope is that the sum of usage of the AZ-localized objects matches the reported usage total.
// If this is the case, the entry for AvailabilityZoneUnknown will be removed entirely once it reaches zero usage.
func (r *ResourceUsageReport) AddLocalizedUsage(az AvailabilityZone, usage uint64) {
	if u := r.PerAZ[AvailabilityZoneUnknown]; u == nil || u.Usage <= usage {
		delete(r.PerAZ, AvailabilityZoneUnknown)
	} else {
		r.PerAZ[AvailabilityZoneUnknown].Usage -= usage
	}

	if _, exists := r.PerAZ[az]; exists {
		r.PerAZ[az].Usage += usage
	} else {
		r.PerAZ[az] = &AZResourceUsageReport{Usage: usage}
	}
}

// RateUsageReport contains usage data for a rate in a single project.
// It appears in type ServiceUsageReport.
type RateUsageReport struct {
	// The keys that are allowed in this map depend on the chosen Topology.
	// See documentation on Topology enum variants for details.
	PerAZ map[AvailabilityZone]*AZRateUsageReport `json:"perAZ"`
}

// AZRateUsageReport contains usage data for a rate in a single project and AZ.
// It appears in type RateUsageReport.
type AZRateUsageReport struct {
	// The amount of usage for this rate. Must be Some() and non-nil if the rate is declared with HasUsage = true.
	// The value Some(nil) is forbidden.
	//
	// For a given rate, project and AZ, this value must only ever increase monotonically over time.
	// If there is the possibility of counter resets or limited retention in the underlying data source, the liquid must add its own logic to guarantee monotonicity.
	// A common strategy is to remember previous measurements in the SerializedState field of type ServiceUsageReport.
	//
	// This field is modeled as a bigint because network rates like "bytes transferred" may easily exceed the range of uint64 over time.
	Usage Option[*big.Int] `json:"usage,omitzero"`
}
