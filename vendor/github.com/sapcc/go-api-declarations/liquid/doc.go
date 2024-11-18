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

// Package liquid contains the API specification for LIQUID (the [Limes] Interface for Quota and Usage Interrogation and Discovery).
//
// [Limes] expects OpenStack services to expose this interface either natively or through an adapter.
// The interface allows Limes to retrieve quota and usage data, and optionally capacity data, from the respective OpenStack service.
// Limes will also use the interface to set quota within the service.
//
// # Naming conventions
//
// Throughout this document:
//   - "LIQUID" (upper case) refers to the REST interface defined by this document.
//   - "A liquid" (lower case) refers to a server implementing LIQUID.
//   - "The liquid's service" refers to the OpenStack service that the liquid is a part of or connected to.
//
// Each liquid provides access to zero or more resources and zero or more rates:
//   - A resource is any countable or measurable kind of entity managed by the liquid's service.
//   - A rate is any countable or measurable series of events or transfers managed by the liquid's service.
//
// Limes discovers liquids through the Keystone service catalog.
// Each liquid should be registered there with a service type that has the prefix "liquid-".
// If a liquid uses vendor-specific APIs to interact with its service, its service type may include the vendor name.
//
// # Inside a resource: Usage, quota, capacity, overcommit
//
// Resources describe objects that are provisioned at some point and then kept around until they are later deleted.
// Examples of resources include VMs in a compute service, volumes in a storage service, or floating IPs in a network service.
// (This does not mean that each individual floating IP is a resource. The entire concept of "floating IPs" is the resource.)
// Resource usage and capacity is always measured at a specific point in time, like for the Prometheus metric type "gauge".
//
// All resources report a usage value for each Keystone project.
// This describes how much of the resource is used by objects created within the project.
// For example, the usage for the compute resource "cores" is the sum of all vCPUs allocated to each VM in that project.
//
// Some resources maintain a quota value for each Keystone project.
// If so, the usage value must be meaningfully connected to the quota value:
// Provisioning of new assets shall be rejected with "quota exceeded" if and only if usage would exceed quota afterwards.
//
// Some resources report a capacity value that applies to the entire OpenStack deployment.
// For example, the capacity for the compute resource "cores" would be the total amount of CPU cores available across all hypervisors.
//
// This capacity value, as it is reported by the liquid, is also called "raw capacity".
// Limes may be configured to apply an "overcommit factor" to obtain an "effective capacity".
// For example, the compute resource "cores" is often overcommitted because most users do not put 100% load on their VMs all the time.
// In this case, quota and usage values are in terms of effective capacity, even though the capacity value is in terms of raw capacity.
//
// Capacity and usage may be AZ-aware, in which case one value will be reported per availability zone (AZ).
// Quota is only optionally modelled as AZ-aware since there are no OpenStack services that support AZ-aware quota at this time.
//
// # Inside a rate: Usage
//
// Rates are measurements that only ever increase over time, similar to the Prometheus metric type "counter".
// For example, if a compute service has the resource "VMs", it might have rates like "VM creations" or "VM deletions".
// Rates describe countable events like in this example, or measurable transfers like "bytes received" or "bytes transferred" on network links.
//
// All rates report a usage value for each Keystone project.
// Usage for each project must increase monotonically over time.
// Usage may be AZ-aware, in which case one value will be reported per availability zone (AZ).
//
// # API structure
//
// LIQUID is structured as a REST-like HTTP API akin to those of the various OpenStack services.
// Like with any other OpenStack API, the client (i.e. Limes) authenticates to the liquid by providing its Keystone token in the HTTP header "X-Auth-Token".
// Requests without a valid token shall be rejected with status 401 (Unauthorized).
// Requests with a valid token that confers insufficient access shall be rejected with status 403 (Forbidden).
//
// Each individual endpoint is documented in a section of this docstring whose title starts with "Endpoint:".
// Unless noted otherwise, a liquid must implement all documented endpoints.
// The full URL of the endpoint is obtained by appending the subpath from the section header to the liquid's base URL from the Keystone service catalog.
//
// The documentation for an endpoint may refer to a request body being expected or a response body being generated on success.
// In all such cases, the request or response body will be encoded as "Content-Type: application/json".
// The structure of the payload must conform to how the referenced Go type would be serialized by the Go standard library's "encoding/json" package.
//
// When producing a successful response, the status code shall be 200 (OK) unless noted otherwise.
// When producing an error response (with a status code between 400 and 599), the liquid shall include a response body of "Content-Type: text/plain" to indicate the error.
//
// # Metrics
//
// While measuring quota, usage and capacity on behalf of Limes, liquids may obtain other metrics that may be useful to report to the OpenStack operator.
// LIQUID offers an optional facility to report metrics like this to Limes as part of the regular quota/usage and capacity reports.
// These metrics will be stored in the Limes database and then collectively forwarded to a metrics database like [Prometheus].
// This delivery method is designed to ensure that liquids can be operated without their own persistent storage.
//
// LIQUID structures metrics in the same way as the [OpenMetrics format] used by Prometheus:
//   - A "metric" is a floating-point-valued measurement with an optional set of labels. A label set is a map of string keys to string values.
//   - A "metric family" is a named set of metrics where the labelset of each metric must have the same keys, but a distinct set of values.
//
// # Endpoint: GET /v1/info
//
// Returns information about the OpenStack service and the resources available within it.
//   - On success, the response body payload must be of type ServiceInfo.
//
// # Endpoint: POST /v1/report-capacity
//
// Reports available capacity across all resources of this service.
//   - The request body payload must be of type ServiceCapacityRequest.
//   - On success, the response body payload must be of type ServiceCapacityReport.
//
// # Endpoint: POST /v1/projects/:uuid/report-usage
//
// Reports usage data (as well as applicable quotas) within a project across all resources of this service.
//   - The ":uuid" parameter in the request path must refer to a project ID known to Keystone.
//   - The request body payload must be of type ServiceUsageRequest.
//   - On success, the response body payload must be of type ServiceUsageReport.
//
// # Endpoint: PUT /v1/projects/:uuid/quota
//
// Updates quota within a project across all resources of this service.
//   - The ":uuid" parameter in the request path must refer to a project ID known to Keystone.
//   - The request body payload must be of type ServiceQuotaRequest.
//   - On success, the response body shall be empty and status 204 (No Content) shall be returned.
//
// [Limes]: https://github.com/sapcc/limes
// [OpenMetrics format]: https://github.com/OpenObservability/OpenMetrics/blob/master/specification/OpenMetrics.md
// [Prometheus]: https://prometheus.io/
package liquid

// ResourceName identifies a resource within a service.
// This type is used to distinguish resource names from other types of string values in function signatures.
type ResourceName string

// RateName identifies a rate within a service.
// This type is used to distinguish rate names from other types of string values in function signatures.
type RateName string
