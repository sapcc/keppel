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
	"slices"
	"time"

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
		WHERE is_managed AND next_account_enforcement_at < $1
		ORDER BY next_account_enforcement_at ASC, name ASC
	`)
	managedAccountEnforcementDoneQuery = sqlext.SimplifyWhitespace(`
		UPDATE accounts SET next_account_enforcement_at = $2 WHERE name = $1
	`)
)

func (j *Janitor) discoverManagedAccount(_ context.Context, _ prometheus.Labels) (accountName string, err error) {
	managedAccountNames, err := j.amd.ManagedAccountNames()
	if err != nil {
		return "", err
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
		return err
	}

	if account == nil {
		accountModel, err := keppel.FindAccount(j.db, accountName)
		if err != nil {
			return err
		}
		// assume the account got already deleted
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}

		_, err = j.db.Exec("UPDATE accounts SET in_maintenance = TRUE WHERE name = $1", accountName)
		if err != nil {
			return err
		}
		// avoid quering the account twice and we set this field just above via SQL
		accountModel.InMaintenance = true

		resp, err := j.processor().DeleteAccount(ctx, *accountModel)
		if err != nil {
			return err
		}
		if resp != nil {
			logg.Error("Deleting account %s failed: %s", accountName, resp.Error)
			return errors.New(resp.Error)
		}
	} else {
		userIdentity := janitorUserIdentity{TaskName: "account-management"}

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

		jsonBytes, err := json.Marshal(securityScanPolicies)
		if err != nil {
			return err
		}

		setCustomFields := func(account *models.Account) error {
			account.IsManaged = true
			account.SecurityScanPoliciesJSON = string(jsonBytes)
			nextAt := j.timeNow().Add(j.addJitter(1 * time.Hour))
			account.NextAccountEnforcementAt = &nextAt
			return nil
		}

		_, rerr := j.processor().CreateOrUpdateAccount(ctx, *account, userIdentity.UserInfo(), janitorDummyRequest, getSubleaseToken, setCustomFields)
		if rerr != nil {
			return rerr
		}
	}

	_, err = j.db.Exec(managedAccountEnforcementDoneQuery, accountName, j.timeNow().Add(j.addJitter(1*time.Hour)))
	return err
}
