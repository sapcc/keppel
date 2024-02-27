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

package keppel

import (
	"github.com/sapcc/go-bits/regexpext"
)

// NewRBACPolicy is a policy granting user-defined access to repos in an account.
// It is stored in serialized form in the RBACPoliciesJSON field of type Account.
//
// TODO: rename to type RBACPolicy after the `rbac_policies` table has been removed
type NewRBACPolicy struct {
	CidrPattern       string                `json:"match_cidr,omitempty"`
	RepositoryPattern regexpext.PlainRegexp `json:"match_repository,omitempty"`
	UserNamePattern   regexpext.PlainRegexp `json:"match_username,omitempty"`
	Permissions       []string              `json:"permissions"`
}
