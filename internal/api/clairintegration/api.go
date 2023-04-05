/*******************************************************************************
*
* Copyright 2021 SAP SE
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

package clairintegration

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/respondwith"

	"github.com/sapcc/keppel/internal/keppel"
)

// API contains state variables used by the Clair API proxy.
type API struct {
	cfg keppel.Configuration
	ad  keppel.AuthDriver
}

// NewAPI constructs a new API instance.
func NewAPI(cfg keppel.Configuration, ad keppel.AuthDriver) *API {
	return &API{cfg, ad}
}

// AddTo implements the api.API interface.
func (a *API) AddTo(r *mux.Router) {
	if a.cfg.ClairClient != nil {
		r.Methods("GET", "HEAD").Path("/clair/{path:.+}").HandlerFunc(a.reverseProxyToClair)
	}
}

func (a *API) reverseProxyToClair(w http.ResponseWriter, r *http.Request) {
	uid, authErr := a.ad.AuthenticateUserFromRequest(r)
	if authErr != nil {
		authErr.WriteAsTextTo(w)
		w.Write([]byte("\n"))
		return
	}

	if uid == nil || !uid.HasPermission(keppel.CanAdministrateKeppel, "") {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	responseBody := make(map[string]interface{})
	err := a.cfg.ClairClient.SendRequest(r.Method, mux.Vars(r)["path"], &responseBody)
	//We could put much more effort into reverse-proxying error responses
	//properly, but since this interface is only intended for when an admin
	//wants to double-check a Clair API response manually, it's okay to just
	//make every error a 500 and give the actual reason in the error message.
	if respondwith.ErrorText(w, err) {
		return
	}

	respondwith.JSON(w, http.StatusOK, responseBody)
}
