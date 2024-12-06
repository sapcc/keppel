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
	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// AuditSecurityScanPolicy is an audittools.Target.
type AuditSecurityScanPolicy struct {
	Account models.Account
	Policy  keppel.SecurityScanPolicy
}

// Render implements the audittools.Target interface.
func (a AuditSecurityScanPolicy) Render() cadf.Resource {
	return cadf.Resource{
		TypeURI:   "docker-registry/account",
		ID:        string(a.Account.Name),
		ProjectID: a.Account.AuthTenantID,
		Attachments: []cadf.Attachment{
			must.Return(cadf.NewJSONAttachment("payload", a.Policy)),
		},
	}
}
