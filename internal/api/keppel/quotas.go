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
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"

	"github.com/sapcc/keppel/internal/keppel"
)

type quotaAndUsage struct {
	Quota uint64 `json:"quota"`
	Usage uint64 `json:"usage"`
}

type quotaResponse struct {
	Manifests quotaAndUsage `json:"manifests"`
}

type justQuota struct {
	Quota uint64 `json:"quota"`
}

type quotaRequest struct {
	Manifests justQuota `json:"manifests"`
}

func (a *API) handleGetQuotas(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/quotas/:auth_tenant_id")
	authTenantID := mux.Vars(r)["auth_tenant_id"]
	authz := a.authenticateRequest(w, r, authTenantScope(keppel.CanViewQuotas, authTenantID))
	if authz == nil {
		return
	}

	quotas, err := keppel.FindQuotas(a.db, authTenantID)
	if respondwith.ErrorText(w, err) {
		return
	}
	if quotas == nil {
		quotas = keppel.DefaultQuotas(authTenantID)
	}

	manifestCount, err := quotas.GetManifestUsage(a.db)
	if respondwith.ErrorText(w, err) {
		return
	}

	respondwith.JSON(w, http.StatusOK, quotaResponse{
		Manifests: quotaAndUsage{
			Quota: quotas.ManifestCount,
			Usage: manifestCount,
		},
	})
}

func (a *API) handlePutQuotas(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/quotas/:auth_tenant_id")
	authTenantID := mux.Vars(r)["auth_tenant_id"]
	authz := a.authenticateRequest(w, r, authTenantScope(keppel.CanChangeQuotas, authTenantID))
	if authz == nil {
		return
	}

	quotas, err := keppel.FindQuotas(a.db, authTenantID)
	if respondwith.ErrorText(w, err) {
		return
	}
	isUpdate := true
	if quotas == nil {
		quotas = keppel.DefaultQuotas(authTenantID)
		isUpdate = false
	}
	quotasBefore := *quotas

	//parse request
	var req quotaRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		http.Error(w, "request body is not valid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	//check usage
	tx, err := a.db.Begin()
	if respondwith.ErrorText(w, err) {
		return
	}
	defer keppel.RollbackUnlessCommitted(tx)

	manifestCount, err := quotas.GetManifestUsage(tx)
	if respondwith.ErrorText(w, err) {
		return
	}
	if req.Manifests.Quota < manifestCount {
		msg := fmt.Sprintf("requested manifest quota (%d) is below usage (%d)",
			req.Manifests.Quota, manifestCount)
		http.Error(w, msg, http.StatusUnprocessableEntity)
		return
	}

	if quotas.ManifestCount != req.Manifests.Quota {
		//apply quotas if necessary
		quotas.ManifestCount = req.Manifests.Quota
		if isUpdate {
			_, err = tx.Update(quotas)
		} else {
			err = tx.Insert(quotas)
		}
		if respondwith.ErrorText(w, err) {
			return
		}
		err = tx.Commit()
		if respondwith.ErrorText(w, err) {
			return
		}

		//record audit event when quotas have changed
		if userInfo := authz.UserIdentity.UserInfo(); userInfo != nil {
			a.auditor.Record(audittools.EventParameters{
				Time:       time.Now(),
				Request:    r,
				User:       userInfo,
				ReasonCode: http.StatusOK,
				Action:     cadf.UpdateAction,
				Target:     AuditQuotas{QuotasBefore: quotasBefore, QuotasAfter: *quotas},
			})
		}
	}

	respondwith.JSON(w, http.StatusOK, quotaResponse{
		Manifests: quotaAndUsage{
			Quota: req.Manifests.Quota,
			Usage: manifestCount,
		},
	})
}
