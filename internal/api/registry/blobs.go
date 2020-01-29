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

package registryv2

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/replication"
)

//This implements the GET/HEAD /v2/<account>/<repository>/blobs/<digest> endpoint.
func (a *API) handleGetOrHeadBlob(w http.ResponseWriter, r *http.Request) {
	account, repoName, _ := a.checkAccountAccess(w, r)
	if account == nil {
		return
	}

	//check our local registry first
	resp := a.proxyRequestToRegistry(w, r, *account)
	if resp == nil {
		return
	}
	responseWasWritten := false

	//if the blob does not exist there, we may have the option of replicating
	//from upstream
	if resp.Resp.StatusCode == http.StatusNotFound && account.UpstreamPeerHostName != "" {
		repo, err := a.db.FindOrCreateRepository(repoName, *account)
		if respondwith.ErrorText(w, err) {
			return
		}

		repl := replication.NewReplicator(a.cfg, a.db, a.orchestrationDriver)
		blob := replication.Blob{
			Account: *account,
			Repo:    *repo,
			Digest:  mux.Vars(r)["digest"],
		}
		responseWasWritten, err = repl.ReplicateBlob(blob, w, r.Method)

		if err != nil {
			if responseWasWritten {
				//we cannot write to `w` if br.Execute() wrote a response there already
				logg.Error("while trying to replicate blob %s in %s/%s: %s",
					blob.Digest, account.Name, repo.Name, err.Error())
			} else if err == replication.ErrConcurrentReplication {
				//special handling for GET during ongoing replication (429 Too Many
				//Requests is not a perfect match, but it's my best guess for getting
				//clients to automatically retry the request after a few seconds)
				w.Header().Set("Retry-After", "10")
				msg := "currently replicating on a different worker, please retry in a few seconds"
				http.Error(w, msg, http.StatusTooManyRequests)
				return
			} else if rerr, ok := err.(*keppel.RegistryV2Error); ok {
				rerr.WriteAsRegistryV2ResponseTo(w)
				return
			} else {
				respondwith.ErrorText(w, err)
				return
			}
		}
	}

	if !responseWasWritten {
		a.proxyResponseToCaller(w, resp)
	}
}

//This implements the POST /v2/<account>/<repository>/blobs/uploads/ endpoint.
func (a *API) handleStartBlobUpload(w http.ResponseWriter, r *http.Request) {
	//must be set even for 401 responses!
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	account, repoName, _ := a.checkAccountAccess(w, r)
	if account == nil {
		return
	}

	//forbid pushing into replica accounts
	if account.UpstreamPeerHostName != "" {
		msg := fmt.Sprintf("cannot push into replica account (push to %s/%s/%s instead!)",
			account.UpstreamPeerHostName, account.Name, repoName,
		)
		keppel.ErrDenied.With(msg).WriteAsRegistryV2ResponseTo(w)
		return
	}

	//only allow new blob uploads when there is enough quota to push a manifest
	//
	//This is not strictly necessary to enforce the manifest quota, but it's
	//useful to avoid the accumulation of unreferenced blobs in the account's
	//backing storage.
	quotas, err := a.db.FindQuotas(account.AuthTenantID)
	if respondwith.ErrorText(w, err) {
		return
	}
	if quotas == nil {
		quotas = keppel.DefaultQuotas(account.AuthTenantID)
	}
	manifestUsage, err := quotas.GetManifestUsage(a.db)
	if respondwith.ErrorText(w, err) {
		return
	}
	if manifestUsage >= quotas.ManifestCount {
		msg := fmt.Sprintf("manifest quota exceeded (quota = %d, usage = %d)",
			quotas.ManifestCount, manifestUsage,
		)
		keppel.ErrDenied.With(msg).WithStatus(http.StatusConflict).WriteAsRegistryV2ResponseTo(w)
		return
	}

	resp := a.proxyRequestToRegistry(w, r, *account)
	if resp == nil {
		return
	}
	a.proxyResponseToCaller(w, resp)
}
