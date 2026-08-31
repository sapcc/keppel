// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/jobloop"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sqlext"
	"go.xyrillian.de/gg/errext"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

var accountAnnouncementSearchQuery = sqlext.SimplifyWhitespace(`
	SELECT * FROM accounts
		WHERE next_federation_announcement_at IS NULL OR next_federation_announcement_at < $1
	-- accounts without any announcements first, then sorted by last announcement
	ORDER BY next_federation_announcement_at IS NULL DESC, next_federation_announcement_at ASC
	-- only one account at a time
	LIMIT 1
`)

var accountAnnouncementDoneQuery = sqlext.SimplifyWhitespace(`
	UPDATE accounts SET next_federation_announcement_at = $2 WHERE name = $1
`)

// AccountFederationAnnouncementJob is a jobloop.Job. Each task finds an account that has not been
// announced to the FederationDriver in more than an hour, and announces it. If
// no accounts need to be announced, sql.ErrNoRows is returned to instruct the
// caller to slow down.
func (j *Janitor) AccountFederationAnnouncementJob(registerer prometheus.Registerer) jobloop.Job { //nolint: dupl // interface implementation of different things
	return (&jobloop.ProducerConsumerJob[models.Account]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "account federation announcement",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_account_federation_announcements",
				Help: "Counter for announcements of existing accounts to the federation driver.",
			},
		},
		DiscoverTask: func(ctx context.Context, _ prometheus.Labels) (models.Account, error) {
			return models.AccountStore.SelectOne(ctx, j.db, accountAnnouncementSearchQuery, j.timeNow())
		},
		ProcessTask: j.announceAccountToFederation,
	}).Setup(registerer)
}

func (j *Janitor) announceAccountToFederation(ctx context.Context, account models.Account, labels prometheus.Labels) error {
	err := j.fd.RecordExistingAccount(ctx, account.Reduced(), j.timeNow())
	if err != nil {
		// since the announcement is not critical for day-to-day operation, we
		// accept that it can fail and move on regardless
		logg.Error("cannot announce account %q to federation: %s", account.Name, err.Error())
	}

	_, err = j.db.Exec(accountAnnouncementDoneQuery, account.Name, j.timeNow().Add(j.addJitter(1*time.Hour)))
	return err
}

var accountPlatformFilterSyncSearchQuery = sqlext.SimplifyWhitespace(`
	SELECT * FROM accounts
		WHERE upstream_peer_hostname != ''
		AND next_platform_filter_sync_at <= $1
	ORDER BY next_platform_filter_sync_at ASC
	LIMIT 1
`)

var accountPlatformFilterSyncDoneQuery = sqlext.SimplifyWhitespace(`
	UPDATE accounts SET next_platform_filter_sync_at = $2 WHERE name = $1
`)

var accountPlatformFilterUpdateQuery = sqlext.SimplifyWhitespace(`
	UPDATE accounts SET platform_filter = $2, next_platform_filter_sync_at = $3 WHERE name = $1
`)

// AccountPlatformFilterSyncJob is a jobloop.Job. Each task finds a replica account
// whose platform filter has not been checked against the primary account recently,
// and updates the local platform filter if necessary.
func (j *Janitor) AccountPlatformFilterSyncJob(registerer prometheus.Registerer) jobloop.Job { //nolint: dupl // interface implementation of different things
	return (&jobloop.ProducerConsumerJob[models.Account]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "account platform filter sync",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_account_platform_filter_syncs",
				Help: "Counter for syncs of platform filters on internal replica accounts against their primary accounts.",
			},
		},
		DiscoverTask: func(ctx context.Context, _ prometheus.Labels) (models.Account, error) {
			return models.AccountStore.SelectOne(ctx, j.db, accountPlatformFilterSyncSearchQuery, j.timeNow())
		},
		ProcessTask: j.syncAccountPlatformFilter,
	}).Setup(registerer)
}

func (j *Janitor) syncAccountPlatformFilter(ctx context.Context, account models.Account, labels prometheus.Labels) error {
	nextSyncAt := j.timeNow().Add(j.addJitter(1 * time.Hour))

	peer, err := keppel.FindPeer(ctx, j.db, account.UpstreamPeerHostName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = fmt.Errorf("cannot sync platform filter for account %q: unknown peer %q", account.Name, account.UpstreamPeerHostName)
			_, err2 := j.db.Exec(accountPlatformFilterSyncDoneQuery, account.Name, nextSyncAt)
			return errext.WithCleanup(err, "next_platform_filter_sync_at update", err2)
		}
		return fmt.Errorf("cannot find peer %q for account %q: %w", account.UpstreamPeerHostName, account.Name, err)
	}

	upstreamPlatformFilter, err := j.processor().GetPlatformFilterFromPrimaryAccount(ctx, peer, account)
	if err != nil {
		err = fmt.Errorf("cannot sync platform filter for account %q from peer %q: %s", account.Name, account.UpstreamPeerHostName, err.Error())
		_, err2 := j.db.Exec(accountPlatformFilterSyncDoneQuery, account.Name, nextSyncAt)
		return errext.WithCleanup(err, "next_platform_filter_sync_at update", err2)
	}

	if account.PlatformFilter.IsEqualTo(upstreamPlatformFilter) {
		_, err = j.db.Exec(accountPlatformFilterSyncDoneQuery, account.Name, nextSyncAt)
		return err
	}

	logg.Info("updating platform filter for account %q from peer %q", account.Name, account.UpstreamPeerHostName)
	_, err = j.db.Exec(accountPlatformFilterUpdateQuery, account.Name, upstreamPlatformFilter, nextSyncAt)
	return err
}
