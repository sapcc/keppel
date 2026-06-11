// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package liquid

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-api-declarations/liquid"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/errext"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"
	. "go.xyrillian.de/gg/option"
	"go.xyrillian.de/oblast"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/processor"
)

var liquidInfoVersion int64 = time.Now().Unix()

// API contains state variables used by the Liquid API implementation.
type API struct {
	cfg        keppel.Configuration
	authDriver keppel.AuthDriver
	sd         keppel.StorageDriver
	db         *oblast.DB
	auditor    audittools.Auditor
	// non-pure functions that can be replaced by deterministic doubles for unit tests
	timeNow func() time.Time
}

// NewLiquidAPI constructs a new LiquidAPI instance.
func NewLiquidAPI(cfg keppel.Configuration, ad keppel.AuthDriver, sd keppel.StorageDriver, db *oblast.DB, auditor audittools.Auditor) *API {
	return &API{cfg, ad, sd, db, auditor, time.Now}
}

// AddTo implements the LiquidAPI interface.
func (a *API) AddTo(r *mux.Router) {
	// Besides the native Keppel API, this handler also implements LIQUID.
	// Ref: <https://pkg.go.dev/github.com/sapcc/go-api-declarations/liquid>
	r.Methods("GET").Path("/liquid/v1/info").HandlerFunc(a.handleLiquidGetInfo)
	r.Methods("POST").Path("/liquid/v1/report-capacity").HandlerFunc(a.handleLiquidReportCapacity)
	r.Methods("POST").Path("/liquid/v1/projects/{auth_tenant_id}/report-usage").HandlerFunc(a.handleLiquidReportUsage)
	r.Methods("PUT").Path("/liquid/v1/projects/{auth_tenant_id}/quota").HandlerFunc(a.handleLiquidSetQuota)
}

func (a *API) processor() *processor.Processor {
	return processor.New(a.cfg, a.db, a.sd, nil, a.auditor, nil, a.timeNow)
}

// TODO: remove `w` argument and return errors using respondwith.CustomStatus(), like in findAccountFromRequest()
func (a *API) authenticateRequest(w http.ResponseWriter, r *http.Request, ss auth.ScopeSet) *auth.Authorization {
	authz, _, rerr := auth.IncomingRequest{
		HTTPRequest:        r,
		Scopes:             ss,
		CorrectlyReturn403: true,
	}.Authorize(r.Context(), a.cfg, a.authDriver, a.db)
	if rerr != nil {
		rerr.WriteAsTextTo(w)
		return nil
	}
	return authz
}

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

func decodeJSONRequestBody(body io.Reader, target any) error {
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&target)
	if err != nil {
		err = fmt.Errorf("request body is not valid JSON: %w", err)
		return respondwith.CustomStatus(http.StatusBadRequest, err)
	}
	return nil
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

func authTenantScope(perm keppel.Permission, authTenantID string) auth.ScopeSet {
	return auth.NewScopeSet(auth.Scope{
		ResourceType: "keppel_auth_tenant",
		ResourceName: authTenantID,
		Actions:      []string{string(perm)},
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
		qr.Manifests = Some(processor.SingleQuotaRequestUInt{
			Quota: res.Quota,
		})
	}

	if res, ok := req.Resources["capacity"]; ok {
		qr.Bytes = Some(processor.SingleQuotaRequestInt{
			Quota: int64(res.Quota), //nolint:gosec // quota is admin controlled
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
			Quota: Some(res.Quota),
			PerAZ: liquid.InAnyAZ(liquid.AZResourceUsageReport{
				Usage: res.Usage,
			}),
		}
	}

	return su
}
