// SPDX-FileCopyrightText: 2021 SAP SE
// SPDX-License-Identifier: Apache-2.0

package peerv1

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/errext"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"

	"github.com/sapcc/keppel/internal/client"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// Implementation for the GET /peer/v1/delegatedpull/:hostname/v2/:repo/manifests/:reference endpoint.
func (a *API) handleDelegatedPullManifest(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/peer/v1/delegatedpull/:hostname/v2/:repo/manifests/:reference")
	peer := a.authenticateRequest(w, r)
	if peer == nil {
		return
	}

	// pass through some headers from the original request
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
		UserName: r.Header.Get("X-Keppel-Delegated-Pull-Username"), // may be empty
		Password: r.Header.Get("X-Keppel-Delegated-Pull-Password"), // may be empty
	}
	ref := models.ParseManifestReference(vars["reference"])
	manifestBytes, manifestMediaType, err := rc.DownloadManifest(r.Context(), ref, &opts)

	if err != nil {
		if rerr, ok := errext.As[*keppel.RegistryV2Error](err); ok {
			rerr.WriteAsRegistryV2ResponseTo(w, r)
			return
		} else {
			respondwith.ErrorText(w, err)
			return
		}
	}

	w.Header().Set("Content-Type", manifestMediaType)
	w.Header().Set("Content-Length", strconv.Itoa(len(manifestBytes)))
	w.WriteHeader(http.StatusOK)
	w.Write(manifestBytes)
}
