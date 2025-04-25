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
