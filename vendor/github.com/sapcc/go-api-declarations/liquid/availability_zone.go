// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package liquid

import "slices"

// AvailabilityZone is the name of an availability zone.
// Some special values are enumerated below.
type AvailabilityZone string

const (
	// AvailabilityZoneAny marks values that are not bound to a specific AZ.
	AvailabilityZoneAny AvailabilityZone = "any"
	// AvailabilityZoneUnknown marks values that are bound to an unknown AZ.
	AvailabilityZoneUnknown AvailabilityZone = "unknown"
	// AvailabilityZoneTotal is reserved for situations where AZ-aware values need to be stored and it is useful to store the sum across all AZs alongside the AZ-aware values.
	// For example, usage for a project resource could be stored as {"az-one": 10, "az-two": 5, "total": 15}.
	AvailabilityZoneTotal AvailabilityZone = "total"
)

// IsReal returns whether the given AZ value looks like it refers to a real AZ.
// False is returned for the empty string, as well as all of the special values enumerated above.
func (az AvailabilityZone) IsReal() bool {
	switch az {
	case "", AvailabilityZoneAny, AvailabilityZoneUnknown, AvailabilityZoneTotal:
		return false
	default:
		return true
	}
}

// InAnyAZ is a convenience constructor for the PerAZ fields of ResourceCapacityReport and ResourceUsageReport.
// It can be used for non-AZ-aware resources. The provided report will be placed under the AvailabilityZoneAny key.
func InAnyAZ[T any](value T) map[AvailabilityZone]*T {
	return map[AvailabilityZone]*T{AvailabilityZoneAny: &value}
}

// NormalizeAZ takes an AZ name as reported by an OpenStack service and safely casts it into the AvailabilityZone type.
// If the provided raw value is not equal to any of the AZs known to Limes (from the second list), AvailabilityZoneUnknown will be returned.
func NormalizeAZ(rawAZ string, allAZs []AvailabilityZone) AvailabilityZone {
	az := AvailabilityZone(rawAZ)
	if slices.Contains(allAZs, az) {
		return az
	} else {
		return AvailabilityZoneUnknown
	}
}
