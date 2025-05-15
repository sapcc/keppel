// SPDX-FileCopyrightText: 2024 SAP SE
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"fmt"
	"net/http"
	"time"

	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// QuotaResponse is the response body payload for GET or PUT /keppel/v1/quotas/:auth_tenant_id.
type QuotaResponse struct {
	Manifests SingleQuotaResponse `json:"manifests"`
}

// SingleQuotaResponse appears in type QuotaRequest.
type SingleQuotaResponse struct {
	Quota uint64 `json:"quota"`
	Usage uint64 `json:"usage"`
}

// QuotaRequest is the request body payload for PUT /keppel/v1/quotas/:auth_tenant_id.
type QuotaRequest struct {
	Manifests SingleQuotaRequest `json:"manifests"`
}

// SingleQuotaRequest appears in type QuotaRequest.
type SingleQuotaRequest struct {
	Quota uint64 `json:"quota"`
}

// ImpossibleQuotaError is emitted when SetQuotas() fails because the requested quota is impossible.
type ImpossibleQuotaError struct {
	Message string
}

// Error implements the error interface.
func (e ImpossibleQuotaError) Error() string {
	return e.Message
}

// GetQuotas builds a response for GET /keppel/v1/quotas/:auth_tenant_id.
func (p *Processor) GetQuotas(authTenantID string) (*QuotaResponse, error) {
	quotas, err := keppel.FindQuotas(p.db, authTenantID)
	if err != nil {
		return nil, err
	}
	if quotas == nil {
		quotas = models.DefaultQuotas(authTenantID)
	}

	manifestCount, err := keppel.GetManifestUsage(p.db, *quotas)
	if err != nil {
		return nil, err
	}

	return &QuotaResponse{
		Manifests: SingleQuotaResponse{
			Quota: quotas.ManifestCount,
			Usage: manifestCount,
		},
	}, nil
}

// SetQuotas changes quotas for an auth tenant and then renders a response
// for PUT /keppel/v1/quotas/:auth_tenant_id.
func (p *Processor) SetQuotas(authTenantID string, req QuotaRequest, userInfo audittools.UserInfo, r *http.Request) (*QuotaResponse, error) {
	quotas, err := keppel.FindQuotas(p.db, authTenantID)
	if err != nil {
		return nil, err
	}
	isUpdate := true
	if quotas == nil {
		quotas = models.DefaultQuotas(authTenantID)
		isUpdate = false
	}
	quotasBefore := *quotas

	// check usage
	tx, err := p.db.Begin()
	if err != nil {
		return nil, err
	}
	defer sqlext.RollbackUnlessCommitted(tx)

	manifestCount, err := keppel.GetManifestUsage(tx, *quotas)
	if err != nil {
		return nil, err
	}
	if req.Manifests.Quota < manifestCount {
		msg := fmt.Sprintf("requested manifest quota (%d) is below usage (%d)",
			req.Manifests.Quota, manifestCount)
		return nil, ImpossibleQuotaError{Message: msg}
	}

	if quotas.ManifestCount != req.Manifests.Quota {
		// apply quotas if necessary
		quotas.ManifestCount = req.Manifests.Quota
		if isUpdate {
			_, err = tx.Update(quotas)
		} else {
			err = tx.Insert(quotas)
		}
		if err != nil {
			return nil, err
		}
		err = tx.Commit()
		if err != nil {
			return nil, err
		}

		// record audit event when quotas have changed
		if userInfo != nil {
			p.auditor.Record(audittools.Event{
				Time:       time.Now(),
				Request:    r,
				User:       userInfo,
				ReasonCode: http.StatusOK,
				Action:     cadf.UpdateAction,
				Target:     AuditQuotas{QuotasBefore: quotasBefore, QuotasAfter: *quotas},
			})
		}
	}

	return &QuotaResponse{
		Manifests: SingleQuotaResponse{
			Quota: req.Manifests.Quota,
			Usage: manifestCount,
		},
	}, nil
}
