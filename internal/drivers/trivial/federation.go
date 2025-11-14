// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package trivial

import (
	"context"
	"time"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

type federationDriver struct{}

func init() {
	keppel.FederationDriverRegistry.Add(func() keppel.FederationDriver { return &federationDriver{} })
}

// PluginTypeID implements the keppel.FederationDriver interface.
func (federationDriver) PluginTypeID() string { return driverName }

// Init implements the keppel.FederationDriver interface.
func (federationDriver) Init(ctx context.Context, ad keppel.AuthDriver, cfg keppel.Configuration) error {
	return nil
}

// ClaimAccountName implements the keppel.FederationDriver interface.
func (federationDriver) ClaimAccountName(ctx context.Context, account models.Account, subleaseTokenSecret string) (keppel.ClaimResult, error) {
	return keppel.ClaimSucceeded, nil
}

// IssueSubleaseTokenSecret implements the keppel.FederationDriver interface.
func (federationDriver) IssueSubleaseTokenSecret(ctx context.Context, account models.Account) (string, error) {
	return "", nil
}

// ForfeitAccountName implements the keppel.FederationDriver interface.
func (federationDriver) ForfeitAccountName(ctx context.Context, account models.Account) error {
	return nil
}

// RecordExistingAccount implements the keppel.FederationDriver interface.
func (federationDriver) RecordExistingAccount(ctx context.Context, account models.Account, now time.Time) error {
	return nil
}

// FindPrimaryAccount implements the keppel.FederationDriver interface.
func (federationDriver) FindPrimaryAccount(ctx context.Context, accountName models.AccountName) (string, error) {
	return "", keppel.ErrNoSuchPrimaryAccount
}
