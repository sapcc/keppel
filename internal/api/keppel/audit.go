// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

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
