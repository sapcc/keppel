// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package peerv1

import (
	"context"
	"net/http"

	"github.com/sapcc/go-bits/httpapi"
	"go.xyrillian.de/gg/gsql"
	"go.xyrillian.de/gg/pathrouter"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// API contains state variables used by the peer API. This is an internal API
// that is only available to peered Keppel instances.
type API struct {
	cfg keppel.Configuration
	ad  keppel.AuthDriver
	db  *gsql.DB
}

// NewAPI constructs a new API instance.
func NewAPI(cfg keppel.Configuration, ad keppel.AuthDriver, db *gsql.DB) *API {
	return &API{cfg, ad, db}
}

// AddTo implements the api.API interface.
func (a *API) AddTo(c *httpapi.Composer) {
	// All endpoints shall be grouped into /peer/v1/. For the "delegated pull"
	// subset of endpoints, the end of the path reflects the request that we make
	// to upstream, so there is an additional /v2/ in there in reference to the
	// Registry V2 API.
	c.AddTryHandler(pathrouter.Element("peer", pathrouter.Element("v1", pathrouter.Choice(
		pathrouter.Element("delegatedpull", pathrouter.Variable("hostname", pathrouter.Element("v2", pathrouter.CatchAllVariable("repo", pathrouter.Element("manifests", pathrouter.Variable("reference", pathrouter.Handlers(pathrouter.ByMethod{
			http.MethodGet: a.handleDelegatedPullManifest,
		}))))))),
		pathrouter.Element("sync-replica", pathrouter.Variable("account", pathrouter.CatchAllVariable("repo", pathrouter.Handlers(pathrouter.ByMethod{
			http.MethodPost: a.handleSyncReplica,
		})))),
	))))
}

// TODO: remove `w` argument and return errors using respondwith.CustomStatus(), like in findAccountFromRequest()
func (a *API) authenticateRequest(ctx context.Context, w http.ResponseWriter, r *http.Request) *models.Peer {
	authz, _, rerr := auth.IncomingRequest{
		HTTPRequest: r,
		Scopes:      auth.NewScopeSet(auth.PeerAPIScope),
	}.Authorize(ctx, a.cfg, a.ad, a.db)
	if rerr != nil {
		rerr.WriteAsTextTo(w)
		return nil
	}

	uid, ok := authz.UserIdentity.(*auth.PeerUserIdentity)
	if !ok {
		keppel.ErrUnknown.With("unexpected UserIdentity type: %T", authz.UserIdentity).WriteAsTextTo(w)
		return nil
	}

	peer, err := keppel.FindPeer(ctx, a.db, uid.PeerHostName)
	if err != nil {
		keppel.AsRegistryV2Error(err).WriteAsTextTo(w)
		return nil
	}

	return &peer
}
