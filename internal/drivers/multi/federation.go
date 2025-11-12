// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package multi

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

type federationDriver struct {
	// configuration
	DriverConfigs []json.RawMessage `json:"drivers"`

	// state
	Drivers []keppel.FederationDriver `json:"-"`
}

func init() {
	keppel.FederationDriverRegistry.Add(func() keppel.FederationDriver { return &federationDriver{} })
}

// PluginTypeID implements the keppel.FederationDriver interface.
func (fd *federationDriver) PluginTypeID() string { return "multi" }

// Init implements the keppel.FederationDriver interface.
func (fd *federationDriver) Init(ctx context.Context, ad keppel.AuthDriver, cfg keppel.Configuration) (err error) {
	fd.Drivers = make([]keppel.FederationDriver, len(fd.DriverConfigs))
	for idx, configJSON := range fd.DriverConfigs {
		fd.Drivers[idx], err = keppel.NewFederationDriver(ctx, string(configJSON), ad, cfg)
		if err != nil {
			return err
		}
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
func (fd *federationDriver) FindPrimaryAccount(ctx context.Context, accountName models.AccountName) (peerHostName string, err error) {
	return fd.Drivers[0].FindPrimaryAccount(ctx, accountName)
}
