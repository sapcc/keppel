/*******************************************************************************
*
* Copyright 2020 SAP SE
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

package processor

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/client"
	"github.com/sapcc/keppel/internal/keppel"
	"gopkg.in/gorp.v2"
)

//Processor is a higher-level interface wrapping keppel.DB and keppel.StorageDriver.
//It abstracts DB accesses into high-level interactions and keeps DB updates in
//lockstep with StorageDriver accesses.
type Processor struct {
	cfg         keppel.Configuration
	db          *keppel.DB
	sd          keppel.StorageDriver
	icd         keppel.InboundCacheDriver
	auditor     keppel.Auditor
	repoClients map[string]*client.RepoClient //key = account name

	//non-pure functions that can be replaced by deterministic doubles for unit tests
	timeNow           func() time.Time
	generateStorageID func() string
}

//New creates a new Processor.
func New(cfg keppel.Configuration, db *keppel.DB, sd keppel.StorageDriver, icd keppel.InboundCacheDriver, auditor keppel.Auditor) *Processor {
	return &Processor{cfg, db, sd, icd, auditor, make(map[string]*client.RepoClient), time.Now, keppel.GenerateStorageID}
}

//OverrideTimeNow replaces time.Now with a test double.
func (p *Processor) OverrideTimeNow(timeNow func() time.Time) *Processor {
	p.timeNow = timeNow
	return p
}

//OverrideGenerateStorageID replaces keppel.GenerateStorageID with a test double.
func (p *Processor) OverrideGenerateStorageID(generateStorageID func() string) *Processor {
	p.generateStorageID = generateStorageID
	return p
}

//WithLowlevelAccess lets the caller access the low-level interfaces wrapped by
//this Processor instance. The existence of this method means that the
//low-level interfaces are basically public, but having to use this method
//makes it more obvious when code bypasses the interface of Processor.
//
//NOTE: This method is not used widely at the moment because callers usually
//have direct access to `db` and `sd`, but my plan is to convert most or all DB
//accesses into methods on type Processor eventually.
func (p *Processor) WithLowlevelAccess(action func(*keppel.DB, keppel.StorageDriver) error) error {
	return action(p.db, p.sd)
}

//Executes the action callback within a database transaction.  If the action
//callback returns success (i.e. a nil error), the transaction will be
//committed.  If it returns an error or panics, the transaction will be rolled
//back.
func (p *Processor) insideTransaction(action func(*gorp.Transaction) error) error {
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	isCommitted := false

	defer func() {
		if !isCommitted {
			err := tx.Rollback()
			if err != nil {
				logg.Error("implicit rollback failed: " + err.Error())
			}
		}
	}()

	err = action(tx)
	if err != nil {
		return err
	}
	err = tx.Commit()
	if err != nil {
		return err
	}
	isCommitted = true
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// helper functions used by multiple Processor methods

//Returns nil if and only if the user can push another manifest.
func (p *Processor) checkQuotaForManifestPush(account keppel.Account) error {
	//check if user has enough quota to push a manifest
	quotas, err := keppel.FindQuotas(p.db, account.AuthTenantID)
	if err != nil {
		return err
	}
	if quotas == nil {
		quotas = keppel.DefaultQuotas(account.AuthTenantID)
	}
	manifestUsage, err := quotas.GetManifestUsage(p.db)
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

//Takes a repo in a replica account and returns a RepoClient for accessing its
//the upstream repo in the corresponding primary account.
func (p *Processor) getRepoClientForUpstream(account keppel.Account, repo keppel.Repository) (*client.RepoClient, error) {
	//use cached client if possible (this one probably already contains a valid
	//pull token)
	if c, ok := p.repoClients[repo.FullName()]; ok {
		return c, nil
	}

	if account.UpstreamPeerHostName != "" {
		var peer keppel.Peer
		err := p.db.SelectOne(&peer, `SELECT * FROM peers WHERE hostname = $1`, account.UpstreamPeerHostName)
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
