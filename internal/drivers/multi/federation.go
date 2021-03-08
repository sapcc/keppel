/*******************************************************************************
*
* Copyright 2021 SAP SE
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

package multi

import (
	"errors"
	"strings"
	"time"

	"github.com/sapcc/keppel/internal/keppel"
)

type federationDriver struct {
	Drivers []keppel.FederationDriver
}

func init() {
	keppel.RegisterFederationDriver("multi", func(ad keppel.AuthDriver, cfg keppel.Configuration) (keppel.FederationDriver, error) {
		fd := &federationDriver{}
		driverNames := strings.Split(keppel.MustGetenv("KEPPEL_FEDERATION_MULTI_DRIVERS"), ",")
		for _, driverName := range driverNames {
			if driverName == "multi" {
				//prevent infinite loops
				return nil, errors.New(`cannot nest "multi" federation driver within itself`)
			}
			subdriver, err := keppel.NewFederationDriver(strings.TrimSpace(driverName), ad, cfg)
			if err != nil {
				return nil, err
			}
			fd.Drivers = append(fd.Drivers, subdriver)
		}
		return fd, nil
	})
}

//ClaimAccountName implements the keppel.FederationDriver interface.
func (fd *federationDriver) ClaimAccountName(account keppel.Account, authz keppel.Authorization, subleaseTokenSecret string) (keppel.ClaimResult, error) {
	//the primary driver issued the sublease token secret, so this one has to verify it
	claimResult, err := fd.Drivers[0].ClaimAccountName(account, authz, subleaseTokenSecret)
	if err != nil || claimResult != keppel.ClaimSucceeded {
		return claimResult, err
	}

	//all other drivers are just informed that the claim happened
	now := time.Now()
	for _, driver := range fd.Drivers[1:] {
		err := driver.RecordExistingAccount(account, now)
		if err != nil {
			return keppel.ClaimErrored, err
		}
	}

	return keppel.ClaimSucceeded, nil
}

//IssueSubleaseTokenSecret implements the keppel.FederationDriver interface.
func (fd *federationDriver) IssueSubleaseTokenSecret(account keppel.Account) (string, error) {
	return fd.Drivers[0].IssueSubleaseTokenSecret(account)
}

//ForfeitAccountName implements the keppel.FederationDriver interface.
func (fd *federationDriver) ForfeitAccountName(account keppel.Account) error {
	for _, driver := range fd.Drivers {
		err := driver.ForfeitAccountName(account)
		if err != nil {
			return err
		}
	}
	return nil
}

//RecordExistingAccount implements the keppel.FederationDriver interface.
func (fd *federationDriver) RecordExistingAccount(account keppel.Account, now time.Time) error {
	for _, driver := range fd.Drivers {
		err := driver.RecordExistingAccount(account, now)
		if err != nil {
			return err
		}
	}
	return nil
}

//FindPrimaryAccount implements the keppel.FederationDriver interface.
func (fd *federationDriver) FindPrimaryAccount(accountName string) (peerHostName string, err error) {
	return fd.Drivers[0].FindPrimaryAccount(accountName)
}
