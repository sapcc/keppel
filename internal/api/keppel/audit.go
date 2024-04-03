/******************************************************************************
*
*  Copyright 2019 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package keppelv1

import (
	"encoding/json"

	"github.com/sapcc/go-api-declarations/cadf"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// AuditQuotas is an audittools.TargetRenderer.
type AuditQuotas struct {
	QuotasBefore models.Quotas
	QuotasAfter  models.Quotas
}

// Render implements the audittools.TargetRenderer interface.
func (a AuditQuotas) Render() cadf.Resource {
	return cadf.Resource{
		TypeURI:   "docker-registry/project-quota",
		ID:        a.QuotasAfter.AuthTenantID,
		ProjectID: a.QuotasAfter.AuthTenantID,
		Attachments: []cadf.Attachment{
			{
				Name:    "payload-before",
				TypeURI: "mime:application/json",
				Content: quotasToJSON(a.QuotasBefore),
			},
			{
				Name:    "payload",
				TypeURI: "mime:application/json",
				Content: quotasToJSON(a.QuotasAfter),
			},
		},
	}
}

func quotasToJSON(q models.Quotas) string {
	data := struct {
		ManifestCount uint64 `json:"manifests"`
	}{
		ManifestCount: q.ManifestCount,
	}
	buf, _ := json.Marshal(data)
	return string(buf)
}

// AuditSecurityScanPolicy is an audittools.TargetRenderer.
type AuditSecurityScanPolicy struct {
	Account models.Account
	Policy  keppel.SecurityScanPolicy
}

// Render implements the audittools.TargetRenderer interface.
func (a AuditSecurityScanPolicy) Render() cadf.Resource {
	content, _ := json.Marshal(a.Policy)
	return cadf.Resource{
		TypeURI:   "docker-registry/account",
		ID:        a.Account.Name,
		ProjectID: a.Account.AuthTenantID,
		Attachments: []cadf.Attachment{{
			Name:    "payload",
			TypeURI: "mime:application/json",
			Content: string(content),
		}},
	}
}
