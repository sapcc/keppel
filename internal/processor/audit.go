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

package processor

import (
	"github.com/sapcc/go-api-declarations/cadf"

	"github.com/sapcc/keppel/internal/models"
)

// AuditAccount is an audittools.TargetRenderer.
type AuditAccount struct {
	Account models.Account
}

// Render implements the audittools.TargetRenderer interface.
func (a AuditAccount) Render() cadf.Resource {
	res := cadf.Resource{
		TypeURI:   "docker-registry/account",
		ID:        a.Account.Name,
		ProjectID: a.Account.AuthTenantID,
	}

	gcPoliciesJSON := a.Account.GCPoliciesJSON
	if gcPoliciesJSON != "" && gcPoliciesJSON != "[]" {
		res.Attachments = append(res.Attachments, cadf.Attachment{
			Name:    "gc-policies",
			TypeURI: "mime:application/json",
			Content: a.Account.GCPoliciesJSON,
		})
	}

	rbacPoliciesJSON := a.Account.RBACPoliciesJSON
	if rbacPoliciesJSON != "" && rbacPoliciesJSON != "[]" {
		res.Attachments = append(res.Attachments, cadf.Attachment{
			Name:    "rbac-policies",
			TypeURI: "mime:application/json",
			Content: a.Account.RBACPoliciesJSON,
		})
	}

	return res
}
