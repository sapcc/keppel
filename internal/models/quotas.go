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

package models

// Quotas contains a record from the `quotas` table.
type Quotas struct {
	AuthTenantID  string `db:"auth_tenant_id"`
	ManifestCount uint64 `db:"manifests"`
}

// DefaultQuotas creates a new Quotas instance with the default quotas.
func DefaultQuotas(authTenantID string) *Quotas {
	// Right now, the default quota is always 0. The value of having this function
	// is to ensure that we only need to change this place if this ever changes.
	return &Quotas{
		AuthTenantID:  authTenantID,
		ManifestCount: 0,
	}
}
