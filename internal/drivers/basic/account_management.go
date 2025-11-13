// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package basic

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"sync"

	. "github.com/majewsky/gg/option"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// AccountManagementDriver is the account management driver "basic".
type AccountManagementDriver struct {
	// configuration
	ConfigPath            string   `json:"config_path"`
	ProtectedAccountNames []string `json:"protected_accounts"`

	// state
	config AccountConfig
	lock   sync.RWMutex
}

type AccountConfig struct {
	Accounts []Account `json:"accounts"`
}

type Account struct {
	Name                 models.AccountName          `json:"name"`
	AuthTenantID         string                      `json:"auth_tenant_id"`
	GCPolicies           []keppel.GCPolicy           `json:"gc_policies"`
	RBACPolicies         []keppel.RBACPolicy         `json:"rbac_policies"`
	ReplicationPolicy    *keppel.ReplicationPolicy   `json:"replication"`
	SecurityScanPolicies []keppel.SecurityScanPolicy `json:"security_scan_policies"`
	TagPolicies          []keppel.TagPolicy          `json:"tag_policies,omitempty"`
	ValidationPolicy     *keppel.ValidationPolicy    `json:"validation"`
	PlatformFilter       models.PlatformFilter       `json:"platform_filter"`
}

func init() {
	keppel.AccountManagementDriverRegistry.Add(func() keppel.AccountManagementDriver {
		return &AccountManagementDriver{}
	})
}

// PluginTypeID implements the keppel.AccountManagementDriver interface.
func (a *AccountManagementDriver) PluginTypeID() string { return "basic" }

// Init implements the keppel.AccountManagementDriver interface.
func (a *AccountManagementDriver) Init() error {
	if a.ConfigPath == "" {
		return errors.New("missing required field: params.config_path")
	}
	return a.LoadConfig()
}

// ConfigureAccount implements the keppel.AccountManagementDriver interface.
func (a *AccountManagementDriver) ConfigureAccount(accountName models.AccountName) (Option[keppel.Account], []keppel.SecurityScanPolicy, error) {
	a.lock.RLock()
	defer a.lock.RUnlock()

	for _, cfgAccount := range a.config.Accounts {
		if cfgAccount.Name != accountName {
			continue
		}

		account := keppel.Account{
			AuthTenantID:      cfgAccount.AuthTenantID,
			GCPolicies:        cfgAccount.GCPolicies,
			Name:              cfgAccount.Name,
			RBACPolicies:      cfgAccount.RBACPolicies,
			ReplicationPolicy: cfgAccount.ReplicationPolicy,
			TagPolicies:       cfgAccount.TagPolicies,
			ValidationPolicy:  cfgAccount.ValidationPolicy,
			PlatformFilter:    cfgAccount.PlatformFilter,
		}
		return Some(account), cfgAccount.SecurityScanPolicies, nil
	}

	// we didn't find the account, delete it
	if slices.Contains(a.ProtectedAccountNames, string(accountName)) {
		return None[keppel.Account](), nil, errors.New("refusing to delete this account because of explicit protection")
	}
	return None[keppel.Account](), nil, nil
}

// ManagedAccountNames implements the keppel.AccountManagementDriver interface.
func (a *AccountManagementDriver) ManagedAccountNames() ([]models.AccountName, error) {
	err := a.LoadConfig()
	if err != nil {
		return nil, err
	}

	a.lock.RLock()
	defer a.lock.RUnlock()

	var accounts []models.AccountName
	for _, account := range a.config.Accounts {
		accounts = append(accounts, account.Name)
	}

	return accounts, nil
}

// LoadConfig is used by other functions in this driver to read the config file whenever needed.
// It is exposed as a public method because it is also used by the `keppel server validate-config` command.
func (a *AccountManagementDriver) LoadConfig() error {
	reader, err := os.Open(a.ConfigPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var config AccountConfig
	err = decoder.Decode(&config)
	if err != nil {
		return err
	}

	a.lock.Lock()
	defer a.lock.Unlock()
	a.config = config

	return nil
}
