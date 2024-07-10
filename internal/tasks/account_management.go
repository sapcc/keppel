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

	"github.com/sapcc/keppel/internal/auth"
	peerclient "github.com/sapcc/keppel/internal/client/peer"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// EnforceManagedAccounts is a job. Each task creates newly discovered accounts from the driver.
func (j *Janitor) EnforceManagedAccounts(registerer prometheus.Registerer) jobloop.Job {
	return (&jobloop.ProducerConsumerJob[[]string]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "create new managed accounts",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_managed_account_creations",
				Help: "Counter for managed account creations.",
			},
		},
		DiscoverTask: j.discoverManagedAccounts,
		ProcessTask:  j.enforceManagedAccounts,
	}).Setup(registerer)
}

func (j *Janitor) discoverManagedAccounts(_ context.Context, _ prometheus.Labels) (accountNames []string, err error) {
	managedAccountNames, err := j.amd.ManagedAccountNames()
	if err != nil {
		return nil, err
	}

	var existingAccountNames []string
	_, err = j.db.Select(&existingAccountNames, "SELECT name FROM accounts WHERE is_managed = true")
	if err != nil {
		return nil, err
	}

	for _, accountName := range managedAccountNames {
		if !slices.Contains(existingAccountNames, accountName) {
			accountNames = append(accountNames, accountName)
		}
	}

	var toEnforceAccountNames []string
	_, err = j.db.Select(&toEnforceAccountNames, "SELECT name FROM accounts WHERE is_managed = true and next_account_enforcement_at < $1", j.timeNow())
	if err != nil {
		return nil, err
	}
	accountNames = append(accountNames, toEnforceAccountNames...)

	if len(accountNames) == 0 {
		return nil, sql.ErrNoRows
	}

	return accountNames, nil
}

func (j *Janitor) enforceManagedAccounts(ctx context.Context, accountNames []string, labels prometheus.Labels) error {
	for _, accountName := range accountNames {
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

		_, err = j.db.Exec(accountAnnouncementDoneQuery, accountName, j.timeNow().Add(j.addJitter(1*time.Hour)))
		if err != nil {
			return err
		}
	}

	return nil
}
