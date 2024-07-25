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
