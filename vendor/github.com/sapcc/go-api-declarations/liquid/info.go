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

// ServiceInfo is the response payload format for GET /v1/info.
type ServiceInfo struct {
	// This version number shall be increased whenever any part of the ServiceInfo changes.
	//
	// The metadata version is also reported on most other API responses.
	// Limes uses this version number to discover when the metadata has changed and needs to be queried again.
	//
	// There is no prescribed semantics to the value of the version number, except that:
	//   - Changes in ServiceInfo must lead to a monotonic increase of the Version.
	//   - If the contents of ServiceInfo do not change, the Version too shall not change.
	//
	// Our recommendation is to use the UNIX timestamp of the most recent change.
	// If you run multiple replicas of the liquid, take care to ensure that they agree on the Version value.
	Version int64 `json:"version"`

	// Info for each resource that this service provides.
	Resources map[ResourceName]ResourceInfo `json:"resources"`

	// Info for each rate that this service provides.
	Rates map[RateName]RateInfo `json:"rates"`

	// Info for each metric family that is included in a response to a query for cluster capacity.
	CapacityMetricFamilies map[MetricName]MetricFamilyInfo `json:"capacityMetricFamilies"`

	// Info for each metric family that is included in a response to a query for project quota and usage.
	UsageMetricFamilies map[MetricName]MetricFamilyInfo `json:"usageMetricFamilies"`

	// Whether Limes needs to include the ProjectMetadata field in its requests for usage reports.
	UsageReportNeedsProjectMetadata bool `json:"usageReportNeedsProjectMetadata,omitempty"`

	// Whether Limes needs to include the ProjectMetadata field in its quota update requests.
	QuotaUpdateNeedsProjectMetadata bool `json:"quotaUpdateNeedsProjectMetadata,omitempty"`
}

// ResourceInfo describes a resource that a liquid's service provides.
// This type appears in type ServiceInfo.
type ResourceInfo struct {
	// If omitted or empty, the resource is "countable" and any quota or usage values describe a number of objects.
	// If non-empty, the resource is "measured" and quota or usage values are in multiples of the given unit.
	// For example, the compute resource "cores" is countable, but the compute resource "ram" is measured, usually in MiB.
	Unit Unit `json:"unit,omitempty"`

	// Whether the liquid reports capacity for this resource on the cluster level.
	HasCapacity bool `json:"hasCapacity"`

	// Whether Limes needs to include demand statistics for this resource in its requests for a capacity report.
	NeedsResourceDemand bool `json:"needsResourceDemand"`

	// Whether the liquid reports quota for this resource on the project level.
	// If false, only usage is reported on the project level.
	// Limes will abstain from maintaining quota on such resources.
	HasQuota bool `json:"hasQuota"`
}

// RateInfo describes a rate that a liquid's service provides.
// This type appears in type ServiceInfo.
type RateInfo struct {
	// If omitted or empty, the rate is "countable" and usage values describe a number of events.
	// If non-empty, the rate is "measured" and usage values are in multiples of the given unit.
	// For example, the storage rate "volume_creations" is countable, but the network rate "outbound_transfer" is measured, e.g. in bytes.
	Unit Unit `json:"unit,omitempty"`

	// Whether the liquid reports usage for this rate on the project level.
	// This must currently be true because there is no other reason for a rate to exist.
	// This requirement may be relaxed in the future, if LIQUID starts modelling rate limits and there are rates that have limits, but no usage tracking.
	HasUsage bool `json:"hasUsage"`
}

// ProjectMetadata includes metadata about a project from Keystone.
//
// It appears in types ServiceUsageRequest and ServiceQuotaRequest if requested by the ServiceInfo.
type ProjectMetadata struct {
	UUID   string         `json:"uuid"`
	Name   string         `json:"name"`
	Domain DomainMetadata `json:"domain"`
}

// DomainMetadata includes metadata about a domain from Keystone.
//
// It appears in type ProjectMetadata.
type DomainMetadata struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}
