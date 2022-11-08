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

package trivial

import (
	"time"

	"github.com/sapcc/keppel/internal/keppel"
)

type federationDriver struct{}

func init() {
	keppel.FederationDriverRegistry.Add(func() keppel.FederationDriver { return federationDriver{} })
}

// PluginTypeID implements the keppel.FederationDriver interface.
func (federationDriver) PluginTypeID() string { return "trivial" }

// Init implements the keppel.FederationDriver interface.
func (federationDriver) Init(ad keppel.AuthDriver, cfg keppel.Configuration) error {
	return nil
}

// ClaimAccountName implements the keppel.FederationDriver interface.
func (federationDriver) ClaimAccountName(account keppel.Account, subleaseTokenSecret string) (keppel.ClaimResult, error) {
	return keppel.ClaimSucceeded, nil
}

// IssueSubleaseTokenSecret implements the keppel.FederationDriver interface.
func (federationDriver) IssueSubleaseTokenSecret(account keppel.Account) (string, error) {
	return "", nil
}

// ForfeitAccountName implements the keppel.FederationDriver interface.
func (federationDriver) ForfeitAccountName(account keppel.Account) error {
	return nil
}

// RecordExistingAccount implements the keppel.FederationDriver interface.
func (federationDriver) RecordExistingAccount(account keppel.Account, now time.Time) error {
	return nil
}

// FindPrimaryAccount implements the keppel.FederationDriver interface.
func (federationDriver) FindPrimaryAccount(accountName string) (string, error) {
	return "", keppel.ErrNoSuchPrimaryAccount
}
