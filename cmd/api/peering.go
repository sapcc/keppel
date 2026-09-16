// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package apicmd

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/must"
	"github.com/sapcc/go-bits/osext"
	"github.com/sapcc/go-bits/sqlext"
	"go.xyrillian.de/gg/gsql"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/tasks"
)

type peeringConfig []struct {
	Hostname             string `json:"hostname"`
	UseForPullDelegation *bool  `json:"use_for_pull_delegation"`
}

var createOrUpdatePeerQuery = sqlext.SimplifyWhitespace(`
	INSERT INTO peers (hostname, use_for_pull_delegation) VALUES ($1, $2)
		ON CONFLICT (hostname) DO UPDATE SET use_for_pull_delegation = EXCLUDED.use_for_pull_delegation
`)

func runPeering(ctx context.Context, cfg keppel.Configuration, db *gsql.DB) {
	isPeerHostName := make(map[string]bool)

	var peeringCfg peeringConfig
	decoder := json.NewDecoder(strings.NewReader(osext.GetenvOrDefault("KEPPEL_PEERS", "[]")))
	decoder.DisallowUnknownFields()
	must.Succeed(decoder.Decode(&peeringCfg))

	// add missing entries to `peers` table
	for _, peer := range peeringCfg {
		isPeerHostName[peer.Hostname] = true

		useForPullDelegation := true
		if peer.UseForPullDelegation != nil {
			useForPullDelegation = *peer.UseForPullDelegation
		}
		_ = must.Return(db.Exec(createOrUpdatePeerQuery, peer.Hostname, useForPullDelegation))
	}

	// remove old entries from `peers` table
	allPeers := must.Return(models.PeerStore.Select(ctx, db, `SELECT * FROM peers`).Collect())
	for _, peer := range allPeers {
		if !isPeerHostName[peer.HostName] {
			must.Succeed(models.PeerStore.Delete(ctx, db, peer))
		}
	}

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := tasks.IssueNewPasswordForNextPeer(ctx, cfg, db)
				if err != nil {
					logg.Error("cannot issue new peer password: " + err.Error())
				}
			}
		}
	}()
}
