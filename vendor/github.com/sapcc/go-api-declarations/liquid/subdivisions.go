// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package liquid

import (
	"encoding/json"
	"slices"

	. "github.com/majewsky/gg/option"
)

// Subcapacity describes a distinct chunk of capacity for a resource within an AZ.
// It appears in type AZResourceCapacityReport.
//
// A service will only report subcapacities for such resources where there is a useful substructure to report.
// For example:
//   - Nova can report its hypervisors as subcapacities of the "cores" and "ram" resources.
//   - Cinder can report its storage pools as subcapacities of the "capacity" resource.
//
// The required fields are "Capacity" and at least one of "ID" or "Name".
//
// There is no guarantee that the Capacity values of all subcapacities sum up to the total capacity of the resource.
// For example, some subcapacities may be excluded from new provisioning.
// The capacity calculation could then take this into account and exclude unused capacity from the total.
type Subcapacity struct {
	// A machine-readable unique identifier for this subcapacity, if there is one.
	ID string `json:"id,omitempty"`

	// A human-readable unique identifier for this subcapacity, if there is one.
	Name string `json:"name,omitempty"`

	// The amount of capacity in this subcapacity.
	Capacity uint64 `json:"capacity"`

	// How much of the Capacity is used, or None if no usage data is available.
	Usage Option[uint64] `json:"usage,omitzero"`

	// Additional resource-specific attributes.
	// This must be shaped like a map[string]any, but is typed as a raw JSON message.
	// Limes does not touch these attributes and will just pass them on into its users without deserializing it at all.
	Attributes json.RawMessage `json:"attributes,omitempty"`
}

// Clone returns a deep copy of the given Subcapacity.
func (s Subcapacity) Clone() Subcapacity {
	cloned := s
	cloned.Attributes = slices.Clone(s.Attributes)
	return cloned
}

// SubcapacityBuilder is a helper type for building Subcapacity values.
// If the Attributes in a subcapacity are collected over time, it might be more convenient to have them accessible as a structured type.
// Once assembly is complete, the provided methods can be used to obtain the final Subcapacity value.
type SubcapacityBuilder[A any] struct {
	ID         string
	Name       string
	Capacity   uint64
	Usage      Option[uint64]
	Attributes A
}

// Finalize converts this SubcapacityBuilder into a Subcapacity by serializing the Attributes field to JSON.
// If an error is returned, it is from the json.Marshal() step.
func (b SubcapacityBuilder[A]) Finalize() (Subcapacity, error) {
	buf, err := json.Marshal(b.Attributes)
	return Subcapacity{
		ID:         b.ID,
		Name:       b.Name,
		Capacity:   b.Capacity,
		Usage:      b.Usage,
		Attributes: json.RawMessage(buf),
	}, err
}

// Subresource describes a distinct chunk of usage for a resource within a project and AZ.
// It appears in type AZResourceUsageReport.
//
// A service will only report subresources for such resources where there is a useful substructure to report.
// For example, in the Nova resource "instances", each instance is a subresource.
//
// The required fields are "Size" (only for measured resources) and at least one of "ID" or "Name".
type Subresource struct {
	// A machine-readable unique identifier for this subresource, if there is one.
	ID string `json:"id,omitempty"`

	// A human-readable identifier for this subresource, if there is one.
	// Must be unique at least within its project.
	Name string `json:"name,omitempty"`

	// Must be None for counted resources (for which each subresource must be one of the things that is counted).
	// Must be Some for measured resources, and contain the subresource's size in terms of the resource's unit.
	Usage Option[uint64] `json:"usage,omitzero"`

	// Additional resource-specific attributes.
	// This must be shaped like a map[string]any, but is typed as a raw JSON message.
	// Limes does not touch these attributes and will just pass them on into its users without deserializing it at all.
	Attributes json.RawMessage `json:"attributes,omitempty"`
}

// Clone returns a deep copy of the given Subresource.
func (s Subresource) Clone() Subresource {
	cloned := s
	cloned.Attributes = slices.Clone(s.Attributes)
	return cloned
}

// SubresourceBuilder is a helper type for building Subresource values.
// If the Attributes in a subresource are collected over time, it might be more convenient to have them accessible as a structured type.
// Once assembly is complete, the provided methods can be used to obtain the final Subresource value.
type SubresourceBuilder[A any] struct {
	ID         string
	Name       string
	Usage      Option[uint64]
	Attributes A
}

// Finalize converts this SubresourceBuilder into a Subresource by serializing the Attributes field to JSON.
// If an error is returned, it is from the json.Marshal() step.
func (b SubresourceBuilder[A]) Finalize() (Subresource, error) {
	buf, err := json.Marshal(b.Attributes)
	return Subresource{
		ID:         b.ID,
		Name:       b.Name,
		Usage:      b.Usage,
		Attributes: json.RawMessage(buf),
	}, err
}
