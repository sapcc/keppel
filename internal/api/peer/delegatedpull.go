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
	"strconv"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/keppel/internal/client"
	"github.com/sapcc/keppel/internal/keppel"
)

//Implementation for the GET /peer/v1/delegatedpull/:hostname/v2/:repo/manifests/:reference endpoint.
func (a *API) handleDelegatedPullManifest(w http.ResponseWriter, r *http.Request) {
	peer := a.authenticateRequest(w, r)
	if peer == nil {
		return
	}

	//pass through some headers from the original request
	opts := client.DownloadManifestOpts{
		ExtraHeaders: http.Header{
			"Accept": r.Header["Accept"],
		},
	}

	vars := mux.Vars(r)
	rc := client.RepoClient{
		Scheme:   "https",
		Host:     vars["hostname"],
		RepoName: vars["repo"],
		UserName: r.Header.Get("X-Keppel-Delegated-Pull-Username"), //may be empty
		Password: r.Header.Get("X-Keppel-Delegated-Pull-Password"), //may be empty
	}
	manifestBytes, manifestMediaType, err := rc.DownloadManifest(vars["reference"], &opts)
	switch err := err.(type) {
	case nil:
		break
	case *keppel.RegistryV2Error:
		err.WriteAsRegistryV2ResponseTo(w, r)
		return
	default:
		respondwith.ErrorText(w, err)
		return
	}

	w.Header().Set("Content-Type", manifestMediaType)
	w.Header().Set("Content-Length", strconv.Itoa(len(manifestBytes)))
	w.WriteHeader(http.StatusOK)
	w.Write(manifestBytes)
}
