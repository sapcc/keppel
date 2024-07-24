/******************************************************************************
*
*  Copyright 2024 SAP SE
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

package basic

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/sapcc/go-bits/osext"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// AccountManagementDriver is the account management driver "basic".
type AccountManagementDriver struct {
	ConfigPath string
	config     AccountConfig
	lock       sync.RWMutex
}

type AccountConfig struct {
	Accounts []Accounts `json:"accounts"`
}

type Accounts struct {
	Name                 string                      `json:"name"`
	AuthTenantID         string                      `json:"auth_tenant_id"`
	GCPolicies           []keppel.GCPolicy           `json:"gc_policies"`
	RBACPolicies         []keppel.RBACPolicy         `json:"rbac_policies"`
	ReplicationPolicy    *keppel.ReplicationPolicy   `json:"replication"`
	SecurityScanPolicies []keppel.SecurityScanPolicy `json:"security_scan_policies"`
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
	configPath, err := osext.NeedGetenv("KEPPEL_ACCOUNT_MANAGEMENT_CONFIG_PATH")
	if err != nil {
		return err
	}
	a.ConfigPath = configPath
	return a.loadConfig()
}

// ConfigureAccount implements the keppel.AccountManagementDriver interface.
func (a *AccountManagementDriver) ConfigureAccount(accountName string) (*keppel.Account, []keppel.SecurityScanPolicy, error) {
	a.lock.RLock()
	defer a.lock.RUnlock()

	for _, cfgAccount := range a.config.Accounts {
		if cfgAccount.Name != accountName {
			continue
		}

		account := &keppel.Account{
			AuthTenantID:      cfgAccount.AuthTenantID,
			GCPolicies:        cfgAccount.GCPolicies,
			Name:              cfgAccount.Name,
			RBACPolicies:      cfgAccount.RBACPolicies,
			ReplicationPolicy: cfgAccount.ReplicationPolicy,
			ValidationPolicy:  cfgAccount.ValidationPolicy,
			PlatformFilter:    cfgAccount.PlatformFilter,
		}

		return account, cfgAccount.SecurityScanPolicies, nil
	}

	// we didn't find the account, delete it
	return nil, nil, nil
}

// ManagedAccountNames implements the keppel.AccountManagementDriver interface.
func (a *AccountManagementDriver) ManagedAccountNames() ([]string, error) {
	err := a.loadConfig()
	if err != nil {
		return nil, err
	}

	a.lock.RLock()
	defer a.lock.RUnlock()

	var accounts []string
	for _, account := range a.config.Accounts {
		accounts = append(accounts, account.Name)
	}

	return accounts, nil
}

func (a *AccountManagementDriver) loadConfig() error {
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
