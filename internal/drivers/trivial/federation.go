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
	keppel.RegisterFederationDriver("trivial", func(_ keppel.AuthDriver, _ keppel.Configuration) (keppel.FederationDriver, error) {
		return federationDriver{}, nil
	})
}

//ClaimAccountName implements the keppel.FederationDriver interface.
func (federationDriver) ClaimAccountName(account keppel.Account, authz keppel.Authorization, subleaseToken string) (keppel.ClaimResult, error) {
	return keppel.ClaimSucceeded, nil
}

//IssueSubleaseToken implements the keppel.FederationDriver interface.
func (federationDriver) IssueSubleaseToken(account keppel.Account) (string, error) {
	return "", nil
}

//ForfeitAccountName implements the keppel.FederationDriver interface.
func (federationDriver) ForfeitAccountName(account keppel.Account) error {
	return nil
}

//RecordExistingAccount implements the keppel.FederationDriver interface.
func (federationDriver) RecordExistingAccount(account keppel.Account, now time.Time) error {
	return nil
}
