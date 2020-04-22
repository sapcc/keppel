/******************************************************************************
*
*  Copyright 2019 SAP SE
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

package test

import (
	"errors"
	"fmt"
	"time"

	"github.com/sapcc/keppel/internal/keppel"
)

//FederationDriver (driver ID "unittest") is a keppel.FederationDriver for unit tests.
type FederationDriver struct {
	ClaimFailsBecauseOfUserError   bool
	ClaimFailsBecauseOfServerError bool
	NextSubleaseTokenSecretToIssue string
	ValidSubleaseTokenSecrets      map[string]string //maps accountName => subleaseTokenSecret
	RecordedAccounts               []AccountRecordedByFederationDriver
}

//AccountRecordedByFederationDriver appears in type FederationDriver.
type AccountRecordedByFederationDriver struct {
	Account    keppel.Account
	RecordedAt time.Time
}

func init() {
	keppel.RegisterFederationDriver("unittest", func(_ keppel.AuthDriver, _ keppel.Configuration) (keppel.FederationDriver, error) {
		return &FederationDriver{
			ValidSubleaseTokenSecrets: make(map[string]string),
		}, nil
	})
}

//ClaimAccountName implements the keppel.FederationDriver interface.
func (d *FederationDriver) ClaimAccountName(account keppel.Account, authz keppel.Authorization, subleaseTokenSecret string) (keppel.ClaimResult, error) {
	//simulated failures for primary accounts
	if d.ClaimFailsBecauseOfUserError {
		return keppel.ClaimFailed, fmt.Errorf("cannot assign name %q to auth tenant %q", account.Name, account.AuthTenantID)
	}
	if d.ClaimFailsBecauseOfServerError {
		return keppel.ClaimErrored, fmt.Errorf("failed to assign name %q to auth tenant %q", account.Name, account.AuthTenantID)
	}

	//for replica accounts, do the regular sublease-token dance
	if account.UpstreamPeerHostName != "" {
		expectedTokenSecret, exists := d.ValidSubleaseTokenSecrets[account.Name]
		if !exists || subleaseTokenSecret != expectedTokenSecret {
			return keppel.ClaimFailed, errors.New("wrong sublease token")
		}
		//each sublease token can only be used once
		delete(d.ValidSubleaseTokenSecrets, account.Name)
	}

	return keppel.ClaimSucceeded, nil
}

//IssueSubleaseTokenSecret implements the keppel.FederationDriver interface.
func (d *FederationDriver) IssueSubleaseTokenSecret(account keppel.Account) (string, error) {
	//issue each sublease token only once
	t := d.NextSubleaseTokenSecretToIssue
	d.NextSubleaseTokenSecretToIssue = ""
	return t, nil
}

//ForfeitAccountName implements the keppel.FederationDriver interface.
func (d *FederationDriver) ForfeitAccountName(account keppel.Account) error {
	return nil
}

//RecordExistingAccount implements the keppel.FederationDriver interface.
func (d *FederationDriver) RecordExistingAccount(account keppel.Account, now time.Time) error {
	account.AnnouncedToFederationAt = nil // this pointer type is poison for DeepEqual tests

	d.RecordedAccounts = append(d.RecordedAccounts, AccountRecordedByFederationDriver{
		Account:    account,
		RecordedAt: now,
	})
	return nil
}
