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

package peerv1

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/go-bits/sre"
	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
)

//API contains state variables used by the peer API. This is an internal API
//that is only available to peered Keppel instances.
type API struct {
	cfg keppel.Configuration
	db  *keppel.DB
}

//NewAPI constructs a new API instance.
func NewAPI(cfg keppel.Configuration, db *keppel.DB) *API {
	return &API{cfg, db}
}

//AddTo implements the api.API interface.
func (a *API) AddTo(r *mux.Router) {
	//All endpoints shall be grouped into /peer/v1/. For the "delegated pull"
	//subset of endpoints, the end of the path reflects the request that we make
	//to upstream, so there is an additional /v2/ in there in reference to the
	//Registry V2 API.
	r.Methods("GET").Path("/peer/v1/delegatedpull/{hostname}/v2/{repo:.+}/manifests/{reference}").HandlerFunc(a.handleDelegatedPullManifest)
	r.Methods("GET").Path("/peer/v1/account-filter/{account}").HandlerFunc(a.handleAccountFilter)
	r.Methods("POST").Path("/peer/v1/sync-replica/{account}/{repo:.+}").HandlerFunc(a.handleSyncReplica)
}

func (a *API) authenticateRequest(w http.ResponseWriter, r *http.Request) *keppel.Peer {
	//expect basic auth credentials for a replication user
	userName, password, ok := r.BasicAuth()
	if !ok || !strings.HasPrefix(userName, "replication@") {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil
	}
	peerHostName := strings.TrimPrefix(userName, "replication@")

	//check credentials
	peer, err := auth.CheckPeerCredentials(a.db, peerHostName, password)
	if respondwith.ErrorText(w, err) {
		return nil
	}
	if peer == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil
	}

	return peer
}

// AccountFilter endpoint json struct
type AccountFilter struct {
	Filter keppel.PlatformFilter
}

func (a *API) handleAccountFilter(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/peer/v1/account-filter/:account")
	peer := a.authenticateRequest(w, r)
	if peer == nil {
		return
	}

	//find account
	account, err := keppel.FindAccount(a.db, mux.Vars(r)["account"])
	if respondwith.ErrorText(w, err) {
		return
	}
	if account == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	respondwith.JSON(w, http.StatusOK, AccountFilter{Filter: account.PlatformFilter})
}
