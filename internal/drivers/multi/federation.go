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
	"context"
	"errors"
	"strings"
	"time"

	"github.com/sapcc/go-bits/osext"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

type federationDriver struct {
	Drivers []keppel.FederationDriver
}

func init() {
	keppel.FederationDriverRegistry.Add(func() keppel.FederationDriver { return &federationDriver{} })
}

// PluginTypeID implements the keppel.FederationDriver interface.
func (fd *federationDriver) PluginTypeID() string { return "multi" }

// Init implements the keppel.FederationDriver interface.
func (fd *federationDriver) Init(ctx context.Context, ad keppel.AuthDriver, cfg keppel.Configuration) error {
	driverNames := strings.Split(osext.MustGetenv("KEPPEL_FEDERATION_MULTI_DRIVERS"), ",")
	for _, driverName := range driverNames {
		if driverName == "multi" {
			// prevent infinite loops
			return errors.New(`cannot nest "multi" federation driver within itself`)
		}
		subdriver, err := keppel.NewFederationDriver(ctx, strings.TrimSpace(driverName), ad, cfg)
		if err != nil {
			return err
		}
		fd.Drivers = append(fd.Drivers, subdriver)
	}
	return nil
}

// ClaimAccountName implements the keppel.FederationDriver interface.
func (fd *federationDriver) ClaimAccountName(ctx context.Context, account models.Account, subleaseTokenSecret string) (keppel.ClaimResult, error) {
	// the primary driver issued the sublease token secret, so this one has to verify it
	claimResult, err := fd.Drivers[0].ClaimAccountName(ctx, account, subleaseTokenSecret)
	if err != nil || claimResult != keppel.ClaimSucceeded {
		return claimResult, err
	}

	// all other drivers are just informed that the claim happened
	now := time.Now()
	for _, driver := range fd.Drivers[1:] {
		err := driver.RecordExistingAccount(ctx, account, now)
		if err != nil {
			return keppel.ClaimErrored, err
		}
	}

	return keppel.ClaimSucceeded, nil
}

// IssueSubleaseTokenSecret implements the keppel.FederationDriver interface.
func (fd *federationDriver) IssueSubleaseTokenSecret(ctx context.Context, account models.Account) (string, error) {
	return fd.Drivers[0].IssueSubleaseTokenSecret(ctx, account)
}

// ForfeitAccountName implements the keppel.FederationDriver interface.
func (fd *federationDriver) ForfeitAccountName(ctx context.Context, account models.Account) error {
	for _, driver := range fd.Drivers {
		err := driver.ForfeitAccountName(ctx, account)
		if err != nil {
			return err
		}
	}
	return nil
}

// RecordExistingAccount implements the keppel.FederationDriver interface.
func (fd *federationDriver) RecordExistingAccount(ctx context.Context, account models.Account, now time.Time) error {
	for _, driver := range fd.Drivers {
		err := driver.RecordExistingAccount(ctx, account, now)
		if err != nil {
			return err
		}
	}
	return nil
}

// FindPrimaryAccount implements the keppel.FederationDriver interface.
func (fd *federationDriver) FindPrimaryAccount(ctx context.Context, accountName string) (peerHostName string, err error) {
	return fd.Drivers[0].FindPrimaryAccount(ctx, accountName)
}
