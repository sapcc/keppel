// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package liquid

import (
	"encoding/json"
	"slices"

	"github.com/sapcc/go-api-declarations/internal/clone"
)

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

	// Whether Limes needs to include the ProjectMetadata field in its commitment handling requests.
	CommitmentHandlingNeedsProjectMetadata bool `json:"commitmentHandlingNeedsProjectMetadata,omitempty"`
}

// Clone returns a deep copy of the given ServiceInfo.
func (i ServiceInfo) Clone() ServiceInfo {
	cloned := i
	cloned.Resources = clone.MapRecursively(i.Resources)
	cloned.Rates = clone.MapRecursively(i.Rates)
	cloned.CapacityMetricFamilies = clone.MapRecursively(i.CapacityMetricFamilies)
	cloned.UsageMetricFamilies = clone.MapRecursively(i.UsageMetricFamilies)
	return cloned
}

// ResourceInfo describes a resource that a liquid's service provides.
// This type appears in type ServiceInfo.
type ResourceInfo struct {
	// If omitted or empty, the resource is "countable" and any quota or usage values describe a number of objects.
	// If non-empty, the resource is "measured" and quota or usage values are in multiples of the given unit.
	// For example, the compute resource "cores" is countable, but the compute resource "ram" is measured, usually in MiB.
	Unit Unit `json:"unit,omitempty"`

	// How the resource reports usage (and capacity, if any). This field is required, and must contain one of the valid enum variants defined in this package.
	Topology Topology `json:"topology"`

	// Whether the liquid reports capacity for this resource on the cluster level.
	HasCapacity bool `json:"hasCapacity"`

	// Whether Limes needs to include demand statistics for this resource in its requests for a capacity report.
	NeedsResourceDemand bool `json:"needsResourceDemand"`

	// Whether the liquid reports quota for this resource on the project level.
	// If false, only usage is reported on the project level.
	// Limes will abstain from maintaining quota on such resources.
	HasQuota bool `json:"hasQuota"`

	// Whether the liquid takes responsibility for reviewing changes to commitments for this resource.
	// If false, Limes will handle commitments on this resource on its own without involving the liquid.
	// If true, the liquid needs to be prepared to handle commitment-related requests for this resource.
	HandlesCommitments bool `json:"handlesCommitments,omitempty"`

	// Additional resource-specific attributes.
	// For example, a resource for baremetal nodes of a certain flavor might report flavor attributes like the CPU and RAM size here, instead of on subcapacities and subresources, to avoid repetition.
	//
	// This must be shaped like a map[string]any, but is typed as a raw JSON message.
	// Limes does not touch these attributes and will just pass them on into its users without deserializing it at all.
	Attributes json.RawMessage `json:"attributes,omitempty"`
}

// Clone returns a deep copy of the given ResourceInfo.
func (i ResourceInfo) Clone() ResourceInfo {
	cloned := i
	cloned.Attributes = slices.Clone(i.Attributes)
	return cloned
}

// Topology describes how capacity and usage reported by a certain resource is structured.
// Type type appears in type ResourceInfo.
type Topology string

const (
	// FlatTopology is a topology for resources that are not AZ-aware at all.
	// In reports for this resource, PerAZ must contain exactly one key: AvailabilityZoneAny.
	// Any other entry, as well as the absence of AvailabilityZoneAny, will be considered an error by Limes.
	//
	// If the resource sets HasQuota = true, only a flat number will be given, and PerAZ will be null.
	FlatTopology Topology = "flat"

	// AZAwareTopology is a topology for resources that can measure capacity and usage by AZ.
	// In reports for this resource, PerAZ shall contain an entry for each AZ mentioned in the AllAZs key of the request.
	// PerAZ may also include an entry for AvailabilityZoneUnknown as needed.
	// Any other entry (including AvailabilityZoneAny) will be considered an error by Limes.
	//
	// If the resource sets "HasQuota = true", only a flat number will be given, and PerAZ will be null.
	// This behavior matches the AZ-unawareness of quota in most OpenStack services.
	AZAwareTopology Topology = "az-aware"

	// AZSeparatedTopology is like AZAwareTopology, but quota is also AZ-aware.
	// For resources with HasQuota = false, this behaves the same as AZAwareTopology.
	//
	// If the resource sets "HasQuota = true", quota requests will include the PerAZ breakdown.
	// PerAZ will only contain quotas for actual AZs, not for AvailabilityZoneAny or AvailabilityZoneUnknown.
	AZSeparatedTopology Topology = "az-separated"
)

// ResourceTopology is a synonym for Topology.
//
// Deprecated: Use Topology instead.
type ResourceTopology = Topology

const (
	// Deprecated: Use FlatTopology instead.
	FlatResourceTopology ResourceTopology = FlatTopology
	// Deprecated: Use AZAwareTopology instead.
	AZAwareResourceTopology ResourceTopology = AZAwareTopology
	// Deprecated: Use AZSeparatedTopology instead.
	AZSeparatedResourceTopology ResourceTopology = AZSeparatedTopology
)

// IsValid returns whether the given value is a part of the enum.
// This can be used to check unmarshalled values.
func (t Topology) IsValid() bool {
	switch t {
	case FlatTopology, AZAwareTopology, AZSeparatedTopology:
		return true
	default:
		return false
	}
}

// RateInfo describes a rate that a liquid's service provides.
// This type appears in type ServiceInfo.
type RateInfo struct {
	// If omitted or empty, the rate is "countable" and usage values describe a number of events.
	// If non-empty, the rate is "measured" and usage values are in multiples of the given unit.
	// For example, the storage rate "volume_creations" is countable, but the network rate "outbound_transfer" is measured, e.g. in bytes.
	Unit Unit `json:"unit,omitempty"`

	// How the rate reports usage. This field is required, and must contain one of the valid enum variants defined in this package.
	Topology Topology `json:"topology"`

	// Whether the liquid reports usage for this rate on the project level.
	// This must currently be true because there is no other reason for a rate to exist.
	// This requirement may be relaxed in the future, if LIQUID starts modelling rate limits and there are rates that have limits, but no usage tracking.
	HasUsage bool `json:"hasUsage"`
}

// Clone returns a deep copy of the given RateInfo.
func (i RateInfo) Clone() RateInfo {
	// this method is only offered for compatibility with future expansion;
	// right now, all fields are copied by-value automatically
	return i
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
