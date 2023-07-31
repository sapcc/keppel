/******************************************************************************
*
*  Copyright 2019-2020 SAP SE
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
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/opencontainers/go-digest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/logg"
	accept "github.com/timewasted/go-accept-headers"

	"github.com/sapcc/keppel/internal/api"
	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/processor"
)

// This implements the HEAD/GET /v2/<repo>/manifests/<reference> endpoint.
func (a *API) handleGetOrHeadManifest(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/v2/:account/:repo/manifests/:reference")
	account, repo, authz := a.checkAccountAccess(w, r, createRepoIfMissingAndReplica, a.handleGetOrHeadManifestAnycast)
	if account == nil {
		return
	}

	err := api.CheckRateLimit(r, a.rle, *account, authz, keppel.ManifestPullAction, 1)
	if respondWithError(w, r, err) {
		return
	}

	reference := models.ParseManifestReference(mux.Vars(r)["reference"])
	dbManifest, err := a.findManifestInDB(*repo, reference)
	var manifestBytes []byte

	if !errors.Is(err, sql.ErrNoRows) {
		if respondWithError(w, r, err) {
			return
		}
	}

	if errors.Is(err, sql.ErrNoRows) {
		//if the manifest does not exist there, we may have the option of replicating
		//from upstream (as an exception, other Keppels replicating from us always
		//see the true 404 to properly replicate the non-existence of the manifest
		//from this account into the replica account)
		userType := authz.UserIdentity.UserType()
		if (account.UpstreamPeerHostName != "" || account.ExternalPeerURL != "") && !account.InMaintenance && (userType != keppel.PeerUser && userType != keppel.TrivyUser) {
			//when replicating from external, only authenticated users can trigger the replication
			if account.ExternalPeerURL != "" && userType != keppel.RegularUser {
				if !authz.ScopeSet.Contains(auth.Scope{
					ResourceType: "repository",
					ResourceName: repo.FullName(),
					Actions:      []string{"anonymous_first_pull"},
				}) {
					keppel.ErrDenied.With("image does not exist here, and anonymous users may not replicate images").WithStatus(http.StatusForbidden).WriteAsRegistryV2ResponseTo(w, r)
					return
				}
			}

			dbManifest, manifestBytes, err = a.processor().ReplicateManifest(r.Context(), *account, *repo, reference, keppel.AuditContext{
				UserIdentity: authz.UserIdentity,
				Request:      r,
			})
			if respondWithError(w, r, err) {
				return
			}
		} else {
			keppel.ErrManifestUnknown.With("").WithDetail(reference.Tag).WriteAsRegistryV2ResponseTo(w, r)
			return
		}
	} else {
		//if manifest was found in our DB, fetch the contents from the DB (or fall
		//back to the storage if the DB entry is not there for some reason)
		manifestBytes, err = a.getManifestContentFromDB(repo.ID, dbManifest.Digest)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				logg.Info("could not read manifest %s@%s from DB (falling back to read from storage): %s",
					repo.FullName(), dbManifest.Digest, err.Error())
			}
			manifestBytes, err = a.sd.ReadManifest(*account, repo.Name, dbManifest.Digest)
			if respondWithError(w, r, err) {
				return
			}
		}
	}

	//verify Accept header, if any
	if r.Header.Get("Accept") != "" {
		//Most user agents provide a single Accept header with comma-separated
		//entries, but some user agents that exist in the wild provide each entry
		//as a separate Accept header. The accept library only takes a single
		//header, so if multiple headers are given, we join them explicitly.
		//
		//See also: <https://github.com/moby/moby/blob/5e9ecffb4fe966c19b606dc7ccee921de2e8ba31/plugin/fetch_linux.go#L82-L92>
		acceptHeader := strings.Join(r.Header["Accept"], ", ")
		acceptRules := accept.Parse(acceptHeader)

		//does the Accept header cover the manifest itself?
		negotiatedMediaType, err := acceptRules.Negotiate(
			dbManifest.MediaType,
			//go-containerregistry can take any type of manifest when it accepts
			//"application/json" (it also explicitly accepts
			//"application/vnd.docker.distribution.manifest.v2+json" with higher
			//priority, but that doesn't help when we have an image list manifest)
			"application/json",
		)
		if err != nil {
			//the Accept header was malformed
			keppel.ErrManifestUnknown.With(err.Error()).WithStatus(http.StatusBadRequest).WriteAsRegistryV2ResponseTo(w, r)
			return
		}

		if negotiatedMediaType == "" {
			//we cannot serve the manifest itself, but maybe we can redirect into one of the acceptable
			//alternates
			manifestParsed, _, err := keppel.ParseManifest(dbManifest.MediaType, manifestBytes)
			if err != nil {
				keppel.ErrManifestInvalid.With(err.Error()).WriteAsRegistryV2ResponseTo(w, r)
				return
			}
			for _, subManifestDesc := range manifestParsed.AcceptableAlternates(account.PlatformFilter) {
				if acceptRules.Accepts(subManifestDesc.MediaType) {
					url := fmt.Sprintf("/v2/%s/manifests/%s", getRepoNameForURLPath(*repo, authz), subManifestDesc.Digest.String())
					w.Header().Set("Docker-Content-Digest", subManifestDesc.Digest.String())
					w.Header().Set("Location", url)
					w.WriteHeader(http.StatusTemporaryRedirect)
					return
				}
			}

			//there is not even an acceptable alternate, so we need to bail out
			msg := fmt.Sprintf("manifest type %s is not covered by Accept: %s", dbManifest.MediaType, acceptHeader)
			logg.Debug(msg)
			keppel.ErrManifestUnknown.With(msg).WithStatus(http.StatusNotAcceptable).WriteAsRegistryV2ResponseTo(w, r)
			return
		}
	}

	timeToString := func(t time.Time) string {
		return strconv.FormatInt(t.Unix(), 10)
	}

	securityInfo, err := keppel.GetSecurityInfo(a.db, dbManifest.RepositoryID, dbManifest.Digest)
	if !errors.Is(err, sql.ErrNoRows) {
		if respondWithError(w, r, err) {
			return
		}
	}

	//write response
	w.Header().Set("Content-Length", strconv.FormatUint(uint64(len(manifestBytes)), 10))
	w.Header().Set("Content-Type", dbManifest.MediaType)
	w.Header().Set("Docker-Content-Digest", dbManifest.Digest.String())
	if securityInfo != nil {
		w.Header().Set("X-Keppel-Vulnerability-Status", string(securityInfo.VulnerabilityStatus))
	}
	if dbManifest.MinLayerCreatedAt != nil {
		w.Header().Set("X-Keppel-Min-Layer-Created-At", timeToString(*dbManifest.MinLayerCreatedAt))
	}
	if dbManifest.MaxLayerCreatedAt != nil {
		w.Header().Set("X-Keppel-Max-Layer-Created-At", timeToString(*dbManifest.MaxLayerCreatedAt))
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		w.Write(manifestBytes)
	}

	//count the pull unless a special header is set or the pull is performed by Trivy as part of our security scanning
	if r.Method == http.MethodGet && r.Header.Get("X-Keppel-No-Count-Towards-Last-Pulled") != "1" && authz.UserIdentity.UserType() != keppel.TrivyUser {
		l := prometheus.Labels{"account": account.Name, "auth_tenant_id": account.AuthTenantID, "method": "registry-api"}
		api.ManifestsPulledCounter.With(l).Inc()

		//update manifests.last_pulled_at
		_, err := a.db.Exec(
			`UPDATE manifests SET last_pulled_at = $1 WHERE repo_id = $2 AND digest = $3`,
			a.timeNow(), dbManifest.RepositoryID, dbManifest.Digest,
		)

		if err == nil {
			if dbManifest.LastPulledAt != nil && dbManifest.LastPulledAt.Before(a.timeNow().Add(-7*24*time.Hour)) {
				userNameDisplay := authz.UserIdentity.UserName()
				if authz.UserIdentity.UserType() == keppel.AnonymousUser {
					userNameDisplay = "<anonymous>"
				}
				logg.Info("last_pulled_at timestamp of manifest %s@%s got updated by more than 7 days by user %q",
					repo.FullName(), dbManifest.Digest, userNameDisplay)
			}
		} else {
			logg.Error("could not update last_pulled_at timestamp on manifest %s@%s: %s", repo.FullName(), dbManifest.Digest, err.Error())
		}

		//also update tags.last_pulled_at if applicable
		if reference.IsTag() {
			_, err := a.db.Exec(
				`UPDATE tags SET last_pulled_at = $1 WHERE repo_id = $2 AND digest = $3 AND name = $4`,
				a.timeNow(), dbManifest.RepositoryID, dbManifest.Digest, reference.Tag,
			)
			if err != nil {
				logg.Error("could not update last_pulled_at timestamp on tag %s/%s: %s", repo.FullName(), reference.Tag, err.Error())
			}
		}
	}
}

func (a *API) findManifestInDB(repo keppel.Repository, reference models.ManifestReference) (*keppel.Manifest, error) {
	//resolve tag into digest if necessary
	refDigest := reference.Digest
	if reference.IsTag() {
		digestStr, err := a.db.SelectStr(
			`SELECT digest FROM tags WHERE repo_id = $1 AND name = $2`,
			repo.ID, reference.Tag,
		)
		if err != nil {
			return nil, err
		}
		if digestStr == "" {
			return nil, sql.ErrNoRows
		}
		refDigest, err = digest.Parse(digestStr)
		if err != nil {
			return nil, err
		}
	}

	var dbManifest keppel.Manifest
	err := a.db.SelectOne(&dbManifest,
		`SELECT * FROM manifests WHERE repo_id = $1 AND digest = $2`,
		repo.ID, refDigest.String(),
	)
	return &dbManifest, err
}

func (a *API) getManifestContentFromDB(repoID int64, digestStr digest.Digest) ([]byte, error) {
	var result []byte
	err := a.db.SelectOne(&result,
		`SELECT content FROM manifest_contents WHERE repo_id = $1 AND digest = $2`,
		repoID, digestStr,
	)
	return result, err
}

func (a *API) handleGetOrHeadManifestAnycast(w http.ResponseWriter, r *http.Request, info anycastRequestInfo) {
	err := a.cfg.ReverseProxyAnycastRequestToPeer(w, r, info.PrimaryHostName)
	if respondWithError(w, r, err) {
		return
	}
	api.ManifestsPulledCounter.With(info.AsPrometheusLabels()).Inc()
}

// This implements the DELETE /v2/<repo>/manifests/<reference> endpoint.
func (a *API) handleDeleteManifest(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/v2/:account/:repo/manifests/:reference")
	account, repo, authz := a.checkAccountAccess(w, r, failIfRepoMissing, nil)
	if account == nil {
		return
	}

	//delete tag or manifest from the database
	ref := models.ParseManifestReference(mux.Vars(r)["reference"])
	actx := keppel.AuditContext{
		UserIdentity: authz.UserIdentity,
		Request:      r,
	}
	var err error
	if ref.IsTag() {
		err = a.processor().DeleteTag(*account, *repo, ref.Tag, actx)
	} else {
		err = a.processor().DeleteManifest(*account, *repo, ref.Digest, actx)
	}
	if errors.Is(err, sql.ErrNoRows) {
		keppel.ErrManifestUnknown.With("no such manifest").WriteAsRegistryV2ResponseTo(w, r)
		return
	}
	if respondWithError(w, r, err) {
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// This implements the PUT /v2/<repo>/manifests/<reference> endpoint.
func (a *API) handlePutManifest(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/v2/:account/:repo/manifests/:reference")
	account, repo, authz := a.checkAccountAccess(w, r, createRepoIfMissing, nil)
	if account == nil {
		return
	}

	err := api.CheckRateLimit(r, a.rle, *account, authz, keppel.ManifestPushAction, 1)
	if respondWithError(w, r, err) {
		return
	}

	//forbid pushing into replica accounts
	if account.UpstreamPeerHostName != "" {
		msg := fmt.Sprintf("cannot push into replica account (push to %s/%s/%s instead!)",
			account.UpstreamPeerHostName, account.Name, repo.Name,
		)
		keppel.ErrUnsupported.With(msg).WithStatus(http.StatusMethodNotAllowed).WriteAsRegistryV2ResponseTo(w, r)
		return
	}
	if account.ExternalPeerURL != "" {
		msg := fmt.Sprintf("cannot push into external replica account (push to %s/%s instead!)",
			account.ExternalPeerURL, repo.Name,
		)
		keppel.ErrUnsupported.With(msg).WithStatus(http.StatusMethodNotAllowed).WriteAsRegistryV2ResponseTo(w, r)
		return
	}

	//forbid pushing during maintenance
	if account.InMaintenance {
		keppel.ErrUnsupported.With("account is in maintenance").WithStatus(http.StatusMethodNotAllowed).WriteAsRegistryV2ResponseTo(w, r)
		return
	}

	//read manifest from request
	manifestBytes, err := io.ReadAll(r.Body)
	if respondWithError(w, r, err) {
		return
	}

	//validate and store manifest
	ref := models.ParseManifestReference(mux.Vars(r)["reference"])
	manifest, err := a.processor().ValidateAndStoreManifest(*account, *repo, processor.IncomingManifest{
		Reference: ref,
		MediaType: r.Header.Get("Content-Type"),
		Contents:  manifestBytes,
		PushedAt:  a.timeNow(),
	}, keppel.AuditContext{
		UserIdentity: authz.UserIdentity,
		Request:      r,
	})
	if respondWithError(w, r, err) {
		return
	}

	//count the push
	l := prometheus.Labels{"account": account.Name, "auth_tenant_id": account.AuthTenantID, "method": "registry-api"}
	api.ManifestsPushedCounter.With(l).Inc()

	w.Header().Set("Content-Length", "0")
	w.Header().Set("Docker-Content-Digest", manifest.Digest.String())
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/manifests/%s", getRepoNameForURLPath(*repo, authz), manifest.Digest))
	w.WriteHeader(http.StatusCreated)
}
