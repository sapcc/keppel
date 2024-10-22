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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/jobloop"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/auth"
	peerclient "github.com/sapcc/keppel/internal/client/peer"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// EnforceManagedAccounts is a job. Each task creates newly discovered accounts from the driver.
func (j *Janitor) EnforceManagedAccountsJob(registerer prometheus.Registerer) jobloop.Job {
	return (&jobloop.ProducerConsumerJob[models.AccountName]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "create and update managed accounts",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_managed_account_creations",
				Help: "Counter for managed account creations and updates.",
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

func (j *Janitor) discoverManagedAccount(_ context.Context, _ prometheus.Labels) (accountName models.AccountName, err error) {
	managedAccountNames, err := j.amd.ManagedAccountNames()
	if err != nil {
		return "", fmt.Errorf("could not get ManagedAccountNames() from account management driver: %w", err)
	}

	// if there is a managed account that does not exist yet, create it
	var existingAccountNames []models.AccountName
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

func (j *Janitor) enforceManagedAccount(ctx context.Context, accountName models.AccountName, labels prometheus.Labels) error {
	account, securityScanPolicies, err := j.amd.ConfigureAccount(accountName)
	if err != nil {
		return fmt.Errorf("could not ConfigureAccount(%q) in account management driver: %w", accountName, err)
	}

	// the error returned from either tryDeleteManagedAccount or createOrUpdateManagedAccount is the main error that this method returns...
	var nextCheckDuration time.Duration
	if account == nil {
		var accountModel *models.Account
		accountModel, err = keppel.FindAccount(j.db, accountName)
		if err != nil {
			return err
		}
		if errors.Is(err, sql.ErrNoRows) {
			nextCheckDuration = 0 // assume the account got already deleted
		} else {
			actx := keppel.AuditContext{
				UserIdentity: janitorUserIdentity{TaskName: "managed-account-enforcement"},
				Request:      janitorDummyRequest,
			}
			err = j.processor().MarkAccountForDeletion(*accountModel, actx)
			if err == nil {
				nextCheckDuration = 1 * time.Hour // account will be deleted -> defer next check until probably after it was deleted
			} else {
				err = fmt.Errorf("could not mark account %q for deletion: %w", accountName, err)
				nextCheckDuration = 5 * time.Minute // default interval for recheck after error
			}
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

func (j *Janitor) createOrUpdateManagedAccount(ctx context.Context, account keppel.Account, securityScanPolicies []keppel.SecurityScanPolicy) error {
	userIdentity := janitorUserIdentity{TaskName: "account-management"}

	// if the managed account is an internal replica, the processor needs to ask the primary account for a sublease token
	getSubleaseToken := func(peer models.Peer) (keppel.SubleaseToken, error) {
		viewScope := auth.Scope{
			ResourceType: "keppel_account",
			ResourceName: string(account.Name),
			Actions:      []string{"change", "view"},
		}

		client, err := peerclient.New(ctx, j.cfg, peer, viewScope)
		if err != nil {
			return keppel.SubleaseToken{}, err
		}

		return client.GetSubleaseToken(ctx, account.Name)
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
