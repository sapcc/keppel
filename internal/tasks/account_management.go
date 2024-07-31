/******************************************************************************
*
*  Copyright 2024 SAP SE
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

package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/jobloop"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/auth"
	peerclient "github.com/sapcc/keppel/internal/client/peer"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// EnforceManagedAccounts is a job. Each task creates newly discovered accounts from the driver.
func (j *Janitor) EnforceManagedAccountsJob(registerer prometheus.Registerer) jobloop.Job {
	return (&jobloop.ProducerConsumerJob[string]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "create new managed accounts",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_managed_account_creations",
				Help: "Counter for managed account creations.",
			},
		},
		DiscoverTask: j.discoverManagedAccount,
		ProcessTask:  j.enforceManagedAccount,
	}).Setup(registerer)
}

var (
	managedAccountEnforcementSelectQuery = sqlext.SimplifyWhitespace(`
		SELECT name FROM accounts
		WHERE is_managed AND next_enforcement_at < $1
		ORDER BY next_enforcement_at ASC, name ASC
	`)
	managedAccountEnforcementDoneQuery = sqlext.SimplifyWhitespace(`
		UPDATE accounts SET next_enforcement_at = $2 WHERE name = $1
	`)
)

func (j *Janitor) discoverManagedAccount(_ context.Context, _ prometheus.Labels) (accountName string, err error) {
	managedAccountNames, err := j.amd.ManagedAccountNames()
	if err != nil {
		return "", fmt.Errorf("could not get ManagedAccountNames() from account management driver: %w", err)
	}

	// if there is a managed account that does not exist yet, create it
	var existingAccountNames []string
	_, err = j.db.Select(&existingAccountNames, "SELECT name FROM accounts WHERE is_managed")
	if err != nil {
		return "", err
	}
	for _, managedAccountName := range managedAccountNames {
		if !slices.Contains(existingAccountNames, managedAccountName) {
			return managedAccountName, nil
		}
	}

	// otherwise return the next existing managed account that needs to be synced
	err = j.db.SelectOne(&accountName, managedAccountEnforcementSelectQuery, j.timeNow())
	return accountName, err
}

func (j *Janitor) enforceManagedAccount(ctx context.Context, accountName string, labels prometheus.Labels) error {
	account, securityScanPolicies, err := j.amd.ConfigureAccount(accountName)
	if err != nil {
		return fmt.Errorf("could not ConfigureAccount(%q) in account management driver: %w", accountName, err)
	}

	// the error returned from either tryDeleteManagedAccount or createOrUpdateManagedAccount is the main error that this method returns...
	var nextCheckDuration time.Duration
	if account == nil {
		var done bool
		done, err = j.tryDeleteManagedAccount(ctx, accountName)
		switch {
		case done:
			nextCheckDuration = 0 // account has been deleted -> next check not necessary
		case err == nil:
			nextCheckDuration = 1 * time.Minute // we are making progress to delete this account -> recheck soon
		default:
			err = fmt.Errorf("could not delete managed account %q: %w", accountName, err)
			nextCheckDuration = 5 * time.Minute // default interval for recheck after error
		}
	} else {
		err = j.createOrUpdateManagedAccount(ctx, *account, securityScanPolicies)
		if err == nil {
			nextCheckDuration = 0 // CreateOrUpdateAccount has already updated NextEnforcementAt
		} else {
			err = fmt.Errorf("could not configure managed account %q: %w", accountName, err)
			nextCheckDuration = 5 * time.Minute // default interval for recheck after error
		}
	}

	// ...but depending on the outcome, we also update account.NextEnforcementAt as necessary
	if nextCheckDuration == 0 {
		return err
	}
	_, err2 := j.db.Exec(managedAccountEnforcementDoneQuery, accountName, j.timeNow().Add(j.addJitter(nextCheckDuration)))
	if err2 == nil {
		return err
	} else {
		return fmt.Errorf("%w (additional error when writing next_enforcement_at: %s)", err, err2.Error())
	}
}

func (j *Janitor) tryDeleteManagedAccount(ctx context.Context, accountName string) (done bool, err error) {
	accountModel, err := keppel.FindAccount(j.db, accountName)
	if err != nil {
		return false, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		// assume the account got already deleted
		return true, nil
	}

	_, err = j.db.Exec("UPDATE accounts SET in_maintenance = TRUE WHERE name = $1", accountName)
	if err != nil {
		return false, err
	}
	// avoid quering the account twice and we set this field just above via SQL
	accountModel.InMaintenance = true

	proc := j.processor()
	actx := keppel.AuditContext{
		UserIdentity: janitorUserIdentity{TaskName: "account-sync"},
		Request:      janitorDummyRequest,
	}
	resp, err := proc.DeleteAccount(ctx, *accountModel, actx)
	if err != nil {
		return false, err
	}
	if resp == nil {
		// deletion was completed
		return true, nil
	}
	if resp.Error != "" {
		return false, errors.New(resp.Error)
	}

	// deletion was not completed yet -> check if we need to do something on our side
	if resp.RemainingManifests != nil {
		remainder := *resp.RemainingManifests
		logg.Info("cleaning up managed account %q: need to delete %d manifests in this cycle", accountName, remainder.Count)
		for _, rm := range remainder.Next {
			parsedDigest, err := digest.Parse(rm.Digest)
			if err != nil {
				return false, fmt.Errorf("while deleting manifest %q in repository %q: could not parse digest: %w",
					rm.Digest, rm.RepositoryName, err)
			}
			repo, err := keppel.FindRepository(j.db, rm.RepositoryName, *accountModel)
			if err != nil {
				return false, fmt.Errorf("while deleting manifest %q in repository %q: could not find repository in DB: %w",
					rm.Digest, rm.RepositoryName, err)
			}
			err = proc.DeleteManifest(ctx, *accountModel, *repo, parsedDigest, actx)
			if err != nil {
				return false, fmt.Errorf("while deleting manifest %q in repository %q: %w",
					rm.Digest, rm.RepositoryName, err)
			}
		}
	}
	if resp.RemainingBlobs != nil {
		logg.Info("cleaning up managed account %q: waiting for %d blobs to be deleted", accountName, resp.RemainingBlobs.Count)
	}

	// since the deletion was not finished, we will retry in the next cycle
	return false, nil
}

func (j *Janitor) createOrUpdateManagedAccount(ctx context.Context, account keppel.Account, securityScanPolicies []keppel.SecurityScanPolicy) error {
	userIdentity := janitorUserIdentity{TaskName: "account-management"}

	// if the managed account is an internal replica, the processor needs to ask the primary account for a sublease token
	getSubleaseToken := func(peer models.Peer) (string, *keppel.RegistryV2Error) {
		viewScope := auth.Scope{
			ResourceType: "keppel_account",
			ResourceName: account.Name,
			Actions:      []string{"view"},
		}

		client, err := peerclient.New(ctx, j.cfg, peer, viewScope)
		if err != nil {
			return "", keppel.AsRegistryV2Error(err)
		}

		subleaseToken, err := client.GetSubleaseToken(ctx, account.Name)
		if err != nil {
			return "", keppel.AsRegistryV2Error(err)
		}
		return subleaseToken, nil
	}

	// some fields are not contained in `keppel.Account` and must be handled through a custom callback
	jsonBytes, err := json.Marshal(securityScanPolicies)
	if err != nil {
		return err
	}
	setCustomFields := func(account *models.Account) *keppel.RegistryV2Error {
		account.IsManaged = true
		account.SecurityScanPoliciesJSON = string(jsonBytes)
		nextAt := j.timeNow().Add(j.addJitter(1 * time.Hour))
		account.NextEnforcementAt = &nextAt
		return nil
	}

	// create or update account
	_, rerr := j.processor().CreateOrUpdateAccount(ctx, account, userIdentity.UserInfo(), janitorDummyRequest, getSubleaseToken, setCustomFields)
	if rerr != nil {
		return rerr
	}
	return nil
}
