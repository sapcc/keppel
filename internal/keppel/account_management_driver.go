// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	. "github.com/majewsky/gg/option"
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
	// Returns None if the account was managed, and now shall be deleted.
	// The jobloop will clean up the manifests, blobs, repos and the account.
	ConfigureAccount(accountName models.AccountName) (Option[Account], []SecurityScanPolicy, error)

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
//
// The supplied config must be a string of the form {"type":"foobar","params":{...}},
// where `type` is the plugin type ID and `params` is json.Unmarshal()ed into
// the driver instance to supply driver-specific configuration.
func NewAccountManagementDriver(configJSON string) (AccountManagementDriver, error) {
	callInit := func(amd AccountManagementDriver) error {
		return amd.Init()
	}
	return newDriver("KEPPEL_DRIVER_ACCOUNT_MANAGEMENT", AccountManagementDriverRegistry, configJSON, callInit)
}
