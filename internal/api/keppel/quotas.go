// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppelv1

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/errext"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/processor"
)

func (a *API) handleGetQuotas(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/quotas/:auth_tenant_id")
	authTenantID := mux.Vars(r)["auth_tenant_id"]
	authz := a.authenticateRequest(w, r, authTenantScope(keppel.CanViewQuotas, authTenantID))
	if authz == nil {
		return
	}

	resp, err := a.processor().GetQuotas(authTenantID)
	if respondwith.ErrorText(w, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, resp)
}

func (a *API) handlePutQuotas(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/quotas/:auth_tenant_id")
	authTenantID := mux.Vars(r)["auth_tenant_id"]
	authz := a.authenticateRequest(w, r, authTenantScope(keppel.CanChangeQuotas, authTenantID))
	if authz == nil {
		return
	}

	var req processor.QuotaRequest
	ok := decodeJSONRequestBody(w, r.Body, &req)
	if !ok {
		return
	}

	resp, err := a.processor().SetQuotas(authTenantID, req, authz.UserIdentity.UserInfo(), r)
	if iqerr, ok := errext.As[processor.ImpossibleQuotaError](err); ok {
		http.Error(w, iqerr.Message, http.StatusUnprocessableEntity)
		return
	}
	if respondwith.ErrorText(w, err) {
		return
	}

	respondwith.JSON(w, http.StatusOK, resp)
}
