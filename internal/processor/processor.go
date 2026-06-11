// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/sqlext"
	"go.xyrillian.de/oblast"

	"github.com/sapcc/keppel/internal/client"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// Processor is a higher-level interface wrapping oblast.DB and keppel.StorageDriver.
// It abstracts DB accesses into high-level interactions and keeps DB updates in
// lockstep with StorageDriver accesses.
type Processor struct {
	cfg         keppel.Configuration
	db          *oblast.DB
	fd          keppel.FederationDriver
	sd          keppel.StorageDriver
	icd         keppel.InboundCacheDriver
	auditor     audittools.Auditor
	repoClients map[string]*client.RepoClient // key = account name

	// non-pure functions that can be replaced by deterministic doubles for unit tests
	timeNow           func() time.Time
	generateStorageID func() string
}

// New creates a new Processor.
func New(cfg keppel.Configuration, db *oblast.DB, sd keppel.StorageDriver, icd keppel.InboundCacheDriver, auditor audittools.Auditor, fd keppel.FederationDriver, timenow func() time.Time) *Processor {
	return &Processor{cfg, db, fd, sd, icd, auditor, make(map[string]*client.RepoClient), timenow, keppel.GenerateStorageID}
}

// OverrideTimeNow replaces time.Now with a test double.
func (p *Processor) OverrideTimeNow(timeNow func() time.Time) *Processor {
	p.timeNow = timeNow
	return p
}

// OverrideGenerateStorageID replaces keppel.GenerateStorageID with a test double.
func (p *Processor) OverrideGenerateStorageID(generateStorageID func() string) *Processor {
	p.generateStorageID = generateStorageID
	return p
}

// WithLowlevelAccess lets the caller access the low-level interfaces wrapped by
// this Processor instance. The existence of this method means that the
// low-level interfaces are basically public, but having to use this method
// makes it more obvious when code bypasses the interface of Processor.
//
// NOTE: This method is not used widely at the moment because callers usually
// have direct access to `db` and `sd`, but my plan is to convert most or all DB
// accesses into methods on type Processor eventually.
func (p *Processor) WithLowlevelAccess(action func(*oblast.DB, keppel.StorageDriver) error) error {
	return action(p.db, p.sd)
}

// Executes the action callback within a database transaction.  If the action
// callback returns success (i.e. a nil error), the transaction will be
// committed.  If it returns an error or panics, the transaction will be rolled
// back.
func (p *Processor) insideTransaction(ctx context.Context, action func(context.Context, *oblast.Tx) error) error {
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	defer sqlext.RollbackUnlessCommitted(tx)

	err = action(ctx, tx)
	if err != nil {
		return err
	}
	return tx.Commit()
}

////////////////////////////////////////////////////////////////////////////////
// helper functions used by multiple Processor methods

// Returns nil if and only if the user can push another manifest.
func (p *Processor) checkQuotaForManifestPush(ctx context.Context, account models.ReducedAccount) error {
	// check if user has enough quota to push a manifest
	quotas, err := keppel.FindQuotas(ctx, p.db, account.AuthTenantID)
	if errors.Is(err, sql.ErrNoRows) {
		quotas = p.cfg.DefaultQuotas(account.AuthTenantID)
	} else if err != nil {
		return err
	}
	manifestUsage, err := keppel.GetManifestUsage(p.db, quotas)
	if err != nil {
		return err
	}
	if manifestUsage >= quotas.ManifestCount {
		msg := fmt.Sprintf("manifest quota exceeded (quota = %d, usage = %d)",
			quotas.ManifestCount, manifestUsage,
		)
		return keppel.ErrDenied.With(msg).WithStatus(http.StatusConflict)
	}
	return nil
}

// Takes a repo in a replica account and returns a RepoClient for accessing its
// the upstream repo in the corresponding primary account.
func (p *Processor) getRepoClientForUpstream(ctx context.Context, account models.ReducedAccount, repo models.ReducedRepository) (*client.RepoClient, error) {
	// use cached client if possible (this one probably already contains a valid
	// pull token)
	if c, ok := p.repoClients[repo.FullName()]; ok {
		return c, nil
	}

	if account.UpstreamPeerHostName != "" {
		peer, err := keppel.FindPeer(ctx, p.db, account.UpstreamPeerHostName)
		if err != nil {
			return nil, err
		}

		c := &client.RepoClient{
			Scheme:   "https",
			Host:     peer.HostName,
			RepoName: repo.FullName(),
			UserName: "replication@" + p.cfg.APIPublicHostname,
			Password: peer.OurPassword,
		}
		p.repoClients[repo.FullName()] = c
		return c, nil
	}

	if account.ExternalPeerURL != "" {
		c := &client.RepoClient{
			Scheme:   "https",
			UserName: account.ExternalPeerUserName,
			Password: account.ExternalPeerPassword,
		}
		if strings.Contains(account.ExternalPeerURL, "/") {
			fields := strings.SplitN(account.ExternalPeerURL, "/", 2)
			c.Host = fields[0]
			c.RepoName = fmt.Sprintf("%s/%s", fields[1], repo.Name)
		} else {
			c.Host = account.ExternalPeerURL
			c.RepoName = repo.Name
		}
		p.repoClients[repo.FullName()] = c
		return c, nil
	}

	return nil, fmt.Errorf("account %q does not have an upstream", account.Name)
}
