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

	"github.com/gorilla/mux"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
)

// API contains state variables used by the peer API. This is an internal API
// that is only available to peered Keppel instances.
type API struct {
	cfg keppel.Configuration
	ad  keppel.AuthDriver
	db  *keppel.DB
}

// NewAPI constructs a new API instance.
func NewAPI(cfg keppel.Configuration, ad keppel.AuthDriver, db *keppel.DB) *API {
	return &API{cfg, ad, db}
}

// AddTo implements the api.API interface.
func (a *API) AddTo(r *mux.Router) {
	//All endpoints shall be grouped into /peer/v1/. For the "delegated pull"
	//subset of endpoints, the end of the path reflects the request that we make
	//to upstream, so there is an additional /v2/ in there in reference to the
	//Registry V2 API.
	r.Methods("GET").Path("/peer/v1/delegatedpull/{hostname}/v2/{repo:.+}/manifests/{reference}").HandlerFunc(a.handleDelegatedPullManifest)
	r.Methods("POST").Path("/peer/v1/sync-replica/{account}/{repo:.+}").HandlerFunc(a.handleSyncReplica)
}

func (a *API) authenticateRequest(w http.ResponseWriter, r *http.Request) *keppel.Peer {
	authz, rerr := auth.IncomingRequest{
		HTTPRequest: r,
		Scopes:      auth.NewScopeSet(auth.PeerAPIScope),
	}.Authorize(a.cfg, a.ad, a.db)
	if rerr != nil {
		rerr.WriteAsTextTo(w)
		return nil
	}

	uid, ok := authz.UserIdentity.(auth.PeerUserIdentity)
	if !ok {
		keppel.ErrUnknown.With("unexpected UserIdentity type: %T", authz.UserIdentity).WriteAsTextTo(w)
		return nil
	}

	var peer keppel.Peer
	err := a.db.SelectOne(&peer, `SELECT * FROM peers WHERE hostname = $1`, uid.PeerHostName)
	if err != nil {
		keppel.AsRegistryV2Error(err).WriteAsTextTo(w)
		return nil
	}

	return &peer
}
