// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package trivial

import (
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// AccountManagementDriver is the account management driver "trivial".
type AccountManagementDriver struct{}

func init() {
	keppel.AccountManagementDriverRegistry.Add(func() keppel.AccountManagementDriver {
		return &AccountManagementDriver{}
	})
}

// PluginTypeID implements the keppel.AccountManagementDriver interface.
func (a *AccountManagementDriver) PluginTypeID() string { return driverName }

// Init implements the keppel.AccountManagementDriver interface.
func (a *AccountManagementDriver) Init() error {
	return nil
}

// ConfigureAccount implements the keppel.AccountManagementDriver interface.
func (a *AccountManagementDriver) ConfigureAccount(accountName models.AccountName) (*keppel.Account, []keppel.SecurityScanPolicy, error) {
	// if there are any managed accounts, delete them
	return nil, nil, nil
}

// ManagedAccountNames implements the keppel.AccountManagementDriver interface.
func (a *AccountManagementDriver) ManagedAccountNames() ([]models.AccountName, error) {
	// there should be no managed accounts
	return nil, nil
}
