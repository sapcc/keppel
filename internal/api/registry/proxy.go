/******************************************************************************
*
*  Copyright 2018-2019 SAP SE
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

package registryv2

import (
	"net/http"

	"github.com/sapcc/go-bits/respondwith"
)

//This implements the GET /v2/ endpoint.
func (a *API) handleProxyToplevel(w http.ResponseWriter, r *http.Request) {
	//must be set even for 401 responses!
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	if a.requireBearerToken(w, r, nil) == nil {
		return
	}

	//The response is not defined beyond code 200, so reply in the same way as
	//https://registry-1.docker.io/v2/, with an empty JSON object.
	respondwith.JSON(w, http.StatusOK, map[string]interface{}{})
}

//This implements all repository-scoped endpoints that do not have more specific handlers.
func (a *API) handleProxyToAccount(w http.ResponseWriter, r *http.Request) {
	account, _ := a.checkAccountAccess(w, r)
	if account == nil {
		return
	}

	resp := a.proxyRequestToRegistry(w, r, *account)
	if resp == nil {
		return
	}
	a.proxyResponseToCaller(w, resp)
}
