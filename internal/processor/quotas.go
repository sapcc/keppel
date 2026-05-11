// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/sqlext"
	. "go.xyrillian.de/gg/option"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// QuotaResponse is the response body payload for GET or PUT /keppel/v1/quotas/:auth_tenant_id.
type QuotaResponse struct {
	Bytes     Option[SingleQuotaResponse] `json:"bytes,omitzero"`
	Manifests SingleQuotaResponse         `json:"manifests"`
}

// SingleQuotaResponse appears in type QuotaRequest.
type SingleQuotaResponse struct {
	Quota uint64 `json:"quota"`
	Usage uint64 `json:"usage"`
}

// QuotaRequest is the request body payload for PUT /keppel/v1/quotas/:auth_tenant_id.
type QuotaRequest struct {
	Bytes Option[SingleQuotaRequest] `json:"bytes,omitzero"`
	// This field is always required. Option[] is only used to distinguish a quota set to 0 from a missing quota.
	Manifests Option[SingleQuotaRequest] `json:"manifests,omitzero"`
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
func (p *Processor) GetQuotas(ctx context.Context, authTenantID string) (*QuotaResponse, error) {
	quotas, err := keppel.FindQuotas(p.db, authTenantID)
	if errors.Is(err, sql.ErrNoRows) {
		quotas = models.DefaultQuotas(authTenantID, p.cfg.TrackBytesQuota)
	} else if err != nil {
		return nil, err
	}

	manifestCount, err := keppel.GetManifestUsage(p.db, quotas)
	if err != nil {
		return nil, err
	}

	qr := &QuotaResponse{
		Manifests: SingleQuotaResponse{
			Quota: quotas.ManifestCount,
			Usage: manifestCount,
		},
	}

	if p.cfg.TrackBytesQuota {
		bytesCount, err := p.sd.UsedBytes(ctx, authTenantID)
		if err != nil {
			return nil, err
		}

		qr.Bytes = Some(SingleQuotaResponse{
			Quota: quotas.Bytes,
			Usage: bytesCount,
		})
	}

	return qr, nil
}

// SetQuotas changes quotas for an auth tenant and then renders a response
// for PUT /keppel/v1/quotas/:auth_tenant_id.
func (p *Processor) SetQuotas(ctx context.Context, authTenantID string, req QuotaRequest, userInfo audittools.UserInfo, r *http.Request) (*QuotaResponse, error) {
	isUpdate := true
	quotas, err := keppel.FindQuotas(p.db, authTenantID)
	if errors.Is(err, sql.ErrNoRows) {
		quotas = models.DefaultQuotas(authTenantID, p.cfg.TrackBytesQuota)
		isUpdate = false
	} else if err != nil {
		return nil, err
	}
	quotasBefore := quotas

	reqManifests, ok := req.Manifests.Unpack()
	if !ok {
		msg := "request does not contain manifest quota"
		return nil, ImpossibleQuotaError{Message: msg}
	}

	// check usage
	tx, err := p.db.Begin()
	if err != nil {
		return nil, err
	}
	defer sqlext.RollbackUnlessCommitted(tx)

	manifestCount, err := keppel.GetManifestUsage(tx, quotas)
	if err != nil {
		return nil, err
	}
	if reqManifests.Quota < manifestCount {
		msg := fmt.Sprintf("requested manifest quota (%d) is below usage (%d)", reqManifests.Quota, manifestCount)
		return nil, ImpossibleQuotaError{Message: msg}
	}

	var bytesCount uint64
	reqBytes, ok := req.Bytes.Unpack()
	if p.cfg.TrackBytesQuota && !ok {
		msg := "bytes quota is enabled, but request does not contain bytes quota"
		return nil, ImpossibleQuotaError{Message: msg}
	}
	if !p.cfg.TrackBytesQuota && ok {
		msg := "bytes quota is not enabled, but request contains bytes quota"
		return nil, ImpossibleQuotaError{Message: msg}
	}

	if p.cfg.TrackBytesQuota {
		bytesCount, err = p.sd.UsedBytes(ctx, authTenantID)
		if err != nil {
			return nil, err
		}
		if reqBytes.Quota < bytesCount {
			msg := fmt.Sprintf("requested bytes quota (%d) is below usage (%d)", reqBytes.Quota, bytesCount)
			return nil, ImpossibleQuotaError{Message: msg}
		}
	}

	if quotas.ManifestCount != reqManifests.Quota || (p.cfg.TrackBytesQuota && quotas.Bytes != reqBytes.Quota) {
		// apply quotas if necessary
		quotas.ManifestCount = reqManifests.Quota
		if p.cfg.TrackBytesQuota {
			quotas.Bytes = reqBytes.Quota
		}
		if isUpdate {
			_, err = tx.Update(&quotas)
		} else {
			err = tx.Insert(&quotas)
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
				Target:     AuditQuotas{QuotasBefore: quotasBefore, QuotasAfter: quotas},
			})
		}
	}

	qr := &QuotaResponse{
		Manifests: SingleQuotaResponse{
			Quota: reqManifests.Quota,
			Usage: manifestCount,
		},
	}

	if p.cfg.TrackBytesQuota {
		qr.Bytes = Some(SingleQuotaResponse{
			Quota: reqBytes.Quota,
			Usage: bytesCount,
		})
	}

	return qr, nil
}
