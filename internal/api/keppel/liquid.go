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

package keppelv1

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-api-declarations/liquid"
	"github.com/sapcc/go-bits/errext"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/processor"
)

// Increment this whenever the output of handleLiquidGetInfo() changes.
const LiquidInfoVersion int64 = 1

func (a *API) handleLiquidGetInfo(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/liquid/v1/info")
	respondwith.JSON(w, http.StatusOK, liquid.ServiceInfo{
		Version: LiquidInfoVersion,
		Resources: map[liquid.ResourceName]liquid.ResourceInfo{
			"images": {
				Unit:        liquid.UnitNone,
				HasCapacity: false,
				HasQuota:    true,
			},
		},
	})
}

func (a *API) handleLiquidReportCapacity(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/liquid/v1/report-capacity")

	// we don't need the request data, but we check it to satisfy the spec
	var req liquid.ServiceCapacityRequest
	ok := decodeJSONRequestBody(w, r.Body, &req)
	if !ok {
		return
	}

	// but we don't need to do any actual work since nothing reports capacity
	respondwith.JSON(w, http.StatusOK, liquid.ServiceCapacityReport{
		InfoVersion: LiquidInfoVersion,
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
	ok := decodeJSONRequestBody(w, r.Body, &req)
	if !ok {
		return
	}

	resp, err := a.processor().GetQuotas(authTenantID)
	if respondwith.ErrorText(w, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, liquidConvertQuotaResponse(*resp))
}

func pointerTo[T any](value T) *T {
	return &value
}

func (a *API) handleLiquidSetQuota(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/liquid/v1/projects/:auth_tenant_id/quota")
	authTenantID := mux.Vars(r)["auth_tenant_id"]
	authz := a.authenticateRequest(w, r, authTenantScope(keppel.CanChangeQuotas, authTenantID))
	if authz == nil {
		return
	}

	var req liquid.ServiceQuotaRequest
	ok := decodeJSONRequestBody(w, r.Body, &req)
	if !ok {
		return
	}

	_, err := a.processor().SetQuotas(authTenantID, liquidConvertQuotaRequest(req), authz.UserIdentity.UserInfo(), r)
	if iqerr, ok := errext.As[processor.ImpossibleQuotaError](err); ok {
		http.Error(w, iqerr.Message, http.StatusUnprocessableEntity)
		return
	}
	if respondwith.ErrorText(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func liquidConvertQuotaRequest(req liquid.ServiceQuotaRequest) processor.QuotaRequest {
	return processor.QuotaRequest{
		Manifests: processor.SingleQuotaRequest{
			Quota: req.Resources["images"].Quota,
		},
	}
}

func liquidConvertQuotaResponse(resp processor.QuotaResponse) liquid.ServiceUsageReport {
	return liquid.ServiceUsageReport{
		InfoVersion: LiquidInfoVersion,
		Metrics:     map[liquid.MetricName][]liquid.Metric{},
		Resources: map[liquid.ResourceName]*liquid.ResourceUsageReport{
			"images": {
				Quota: pointerTo(int64(resp.Manifests.Quota)),
				PerAZ: liquid.InAnyAZ(liquid.AZResourceUsageReport{
					Usage: resp.Manifests.Usage,
				}),
			},
		},
	}
}
