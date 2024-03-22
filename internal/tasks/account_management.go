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
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/jobloop"
)

// CreateManagedAccounts is a job. Each task creates newly discovered accounts from the driver.
func (j *Janitor) CreateManagedAccounts(registerer prometheus.Registerer) jobloop.Job {
	return (&jobloop.ProducerConsumerJob[[]string]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "create new managed accounts",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_managed_account_creations",
				Help: "Counter for managed account creations.",
			},
		},
		DiscoverTask: j.discoverAccountsToCreate,
		ProcessTask:  j.createNewAccounts,
	}).Setup(registerer)
}

func (j *Janitor) discoverAccountsToCreate(_ context.Context, _ prometheus.Labels) (accounts []string, err error) {
	managedAccounts, err := j.amd.ManagedAccountNames()
	if err != nil {
		return nil, err
	}

	var existingAccounts []string
	_, err = j.db.Select(&existingAccounts, "SELECT name FROM accounts WHERE is_managed = true")
	if err != nil {
		return nil, err
	}

	for _, account := range managedAccounts {
		if !slices.Contains(existingAccounts, account) {
			accounts = append(accounts, account)
		}
	}

	if len(accounts) == 0 {
		return nil, sql.ErrNoRows
	}

	return accounts, nil
}

func (j *Janitor) createNewAccounts(ctx context.Context, accounts []string, labels prometheus.Labels) error {
	_, err = j.db.Exec(accountAnnouncementDoneQuery, account.Name, j.timeNow().Add(j.addJitter(1*time.Hour)))
	return err
}
