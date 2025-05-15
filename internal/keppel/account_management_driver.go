// SPDX-FileCopyrightText: 2024 SAP SE
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"errors"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/pluggable"

	"github.com/sapcc/keppel/internal/models"
)

// AccountManagementDriver is a pluggable interface for receiving account
// configuration from an external system. Accounts can either be managed by
// this driver, or created and maintained by users through the Keppel API.
type AccountManagementDriver interface {
	pluggable.Plugin
	// Init is called before any other interface methods, and allows the plugin to
	// perform first-time initialization.
	Init() error

	// Called by a jobloop for every account every once in a while (e.g. every hour).
	//
	// Returns the desired account configuration if the account is managed.
	// The jobloop will apply the account in the DB accordingly.
	//
	// Returns nil if the account was managed, and now shall be deleted.
	// The jobloop will clean up the manifests, blobs, repos and the account.
	ConfigureAccount(accountName models.AccountName) (*Account, []SecurityScanPolicy, error)

	// Called by a jobloop every once in a while (e.g. every hour).
	//
	// If new names appear in the list, the jobloop will create the
	// respective accounts as configured by ConfigureAccount().
	ManagedAccountNames() ([]models.AccountName, error)
}

// AccountManagementDriverRegistry is a pluggable.Registry for AccountManagementDriver implementations.
var AccountManagementDriverRegistry pluggable.Registry[AccountManagementDriver]

// NewAccountManagementDriver creates a new AuthDriver using one of the plugins registered
// with AccountManagementDriver.
func NewAccountManagementDriver(pluginTypeID string) (AccountManagementDriver, error) {
	logg.Debug("initializing account management driver %q...", pluginTypeID)

	amd := AccountManagementDriverRegistry.Instantiate(pluginTypeID)
	if amd == nil {
		return nil, errors.New("no such account management driver: " + pluginTypeID)
	}
	return amd, amd.Init()
}
