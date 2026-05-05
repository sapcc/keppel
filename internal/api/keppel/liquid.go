// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppelv1

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-api-declarations/liquid"
	"github.com/sapcc/go-bits/errext"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"
	. "go.xyrillian.de/gg/option"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/processor"
)

var liquidInfoVersion int64 = time.Now().Unix()

func (a *API) handleLiquidGetInfo(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/liquid/v1/info")

	si := liquid.ServiceInfo{
		DisplayName: "Container Image Registry",
		Version:     liquidInfoVersion,
		Resources: map[liquid.ResourceName]liquid.ResourceInfo{
			"images": {
				DisplayName: "Images",
				Unit:        liquid.UnitNone,
				Topology:    liquid.FlatTopology,
				HasCapacity: false,
				HasQuota:    true,
			},
		},
	}

	if a.cfg.TrackBytesQuota {
		si.Resources["capacity"] = liquid.ResourceInfo{
			DisplayName: "Capacity",
			Unit:        liquid.UnitBytes,
			Topology:    liquid.FlatTopology,
			HasCapacity: false,
			HasQuota:    true,
		}
	}

	respondwith.JSON(w, http.StatusOK, si)
}

func (a *API) handleLiquidReportCapacity(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/liquid/v1/report-capacity")

	// we don't need the request data, but we check it to satisfy the spec
	var req liquid.ServiceCapacityRequest
	err := decodeJSONRequestBody(r.Body, &req)
	if respondwith.ObfuscatedErrorText(w, err) {
		return
	}

	// but we don't need to do any actual work since nothing reports capacity
	respondwith.JSON(w, http.StatusOK, liquid.ServiceCapacityReport{
		InfoVersion: liquidInfoVersion,
		Resources:   map[liquid.ResourceName]*liquid.ResourceCapacityReport{},
	})
}

func (a *API) handleLiquidReportUsage(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/liquid/v1/projects/:auth_tenant_id/report-usage")
	authTenantID := mux.Vars(r)["auth_tenant_id"]
	authz := a.authenticateRequest(w, r, authTenantScope(keppel.CanViewQuotas, authTenantID))
	if authz == nil {
		return
	}

	// we don't need the request data, but we check it to satisfy the spec
	var req liquid.ServiceUsageRequest
	err := decodeJSONRequestBody(r.Body, &req)
	if respondwith.ObfuscatedErrorText(w, err) {
		return
	}

	resp, err := a.processor().GetQuotas(r.Context(), authTenantID)
	if respondwith.ObfuscatedErrorText(w, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, liquidConvertQuotaResponse(*resp))
}

func (a *API) handleLiquidSetQuota(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/liquid/v1/projects/:auth_tenant_id/quota")
	authTenantID := mux.Vars(r)["auth_tenant_id"]
	authz := a.authenticateRequest(w, r, authTenantScope(keppel.CanChangeQuotas, authTenantID))
	if authz == nil {
		return
	}

	var req liquid.ServiceQuotaRequest
	err := decodeJSONRequestBody(r.Body, &req)
	if respondwith.ObfuscatedErrorText(w, err) {
		return
	}

	_, err = a.processor().SetQuotas(r.Context(), authTenantID, liquidConvertQuotaRequest(req), authz.UserIdentity.UserInfo(), r)
	if iqerr, ok := errext.As[processor.ImpossibleQuotaError](err); ok {
		http.Error(w, iqerr.Message, http.StatusUnprocessableEntity)
		return
	}
	if respondwith.ObfuscatedErrorText(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func liquidConvertQuotaRequest(req liquid.ServiceQuotaRequest) processor.QuotaRequest {
	qr := processor.QuotaRequest{}

	if res, ok := req.Resources["images"]; ok {
		qr.Manifests = Some(processor.SingleQuotaRequest{
			Quota: res.Quota,
		})
	}

	if res, ok := req.Resources["capacity"]; ok {
		qr.Bytes = Some(processor.SingleQuotaRequest{
			Quota: res.Quota,
		})
	}

	return qr
}

func liquidConvertQuotaResponse(resp processor.QuotaResponse) liquid.ServiceUsageReport {
	su := liquid.ServiceUsageReport{
		InfoVersion: liquidInfoVersion,
		Metrics:     map[liquid.MetricName][]liquid.Metric{},
		Resources: map[liquid.ResourceName]*liquid.ResourceUsageReport{
			"images": {
				Quota: Some(int64(resp.Manifests.Quota)), //nolint:gosec // quota is admin controlled
				PerAZ: liquid.InAnyAZ(liquid.AZResourceUsageReport{
					Usage: resp.Manifests.Usage,
				}),
			},
		},
	}

	if res, ok := resp.Bytes.Unpack(); ok {
		su.Resources["capacity"] = &liquid.ResourceUsageReport{
			Quota: Some(int64(res.Quota)), //nolint:gosec // quota is admin controlled
			PerAZ: liquid.InAnyAZ(liquid.AZResourceUsageReport{
				Usage: res.Usage,
			}),
		}
	}

	return su
}
