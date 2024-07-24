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

// AvailabilityZone is the name of an availability zone.
// Some special values are enumerated below.
type AvailabilityZone string

const (
	// AvailabilityZoneAny marks values that are not bound to a specific AZ.
	AvailabilityZoneAny AvailabilityZone = "any"
	// AvailabilityZoneUnknown marks values that are bound to an unknown AZ.
	AvailabilityZoneUnknown AvailabilityZone = "unknown"
)

// InAnyAZ is a convenience constructor for the PerAZ fields of ResourceCapacityReport and ResourceUsageReport.
// It can be used for non-AZ-aware resources. The provided report will be placed under the AvailabilityZoneAny key.
func InAnyAZ[T any](value T) map[AvailabilityZone]*T {
	return map[AvailabilityZone]*T{AvailabilityZoneAny: &value}
}
