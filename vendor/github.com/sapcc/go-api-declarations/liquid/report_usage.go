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

package liquid

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
	ProjectMetadata *ProjectMetadata `json:"projectMetadata,omitempty"`
}

// ServiceUsageReport is the response payload format for POST /v1/projects/:uuid/report-usage.
type ServiceUsageReport struct {
	// The same version number that is reported in the Version field of a GET /v1/info response.
	// This is used to signal to Limes to refetch GET /v1/info after configuration changes.
	InfoVersion int64 `json:"infoVersion"`

	// Must contain an entry for each resource that was declared in type ServiceInfo.
	Resources map[ResourceName]*ResourceUsageReport `json:"resources"`

	// Must contain an entry for each metric family that was declared for usage metrics in type ServiceInfo.
	Metrics map[MetricName][]Metric `json:"metrics"`
}

// ResourceUsageReport contains usage data for a resource in a single project.
// It appears in type ServiceUsageReport.
type ResourceUsageReport struct {
	// If true, this project is forbidden from accessing this resource.
	// This has two consequences:
	//   - If the resource has quota, Limes will never try to assign quota for this resource to this project.
	//   - If the project has no usage in this resource, Limes will hide this resource from project reports.
	Forbidden bool `json:"forbidden"`

	// This shall be null if and only if the resource is declared with "HasQuota = false".
	// A negative value, usually -1, indicates "infinite quota" (i.e., the absence of a quota).
	Quota *int64 `json:"quota,omitempty"`

	// For non-AZ-aware resources, the only entry shall be for AvailabilityZoneAny.
	// Use func InAnyAZ to quickly construct a suitable structure.
	//
	// For AZ-aware resources, there shall be an entry for each AZ mentioned in ServiceUsageRequest.AllAZs.
	// Reports for AZ-aware resources may also include an entry for AvailabilityZoneUnknown as needed.
	// When starting from a non-AZ-aware usage number that is later broken down with AZ-aware data, use func PrepareForBreakdownInto.
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
	PhysicalUsage *uint64 `json:"physicalUsage,omitempty"`

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
