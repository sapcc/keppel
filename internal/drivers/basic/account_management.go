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
	"os"
	"sync"

	"github.com/sapcc/go-bits/osext"

	"github.com/sapcc/keppel/internal/keppel"

	"gopkg.in/yaml.v2"
)

// AccountManagementDriver is the account management driver "basic".
type AccountManagementDriver struct {
	ConfigPath string
	config     AccountConfig
	lock       sync.RWMutex
}

type AccountConfig struct {
	Accounts []Accounts `yaml:"accounts"`
}

type Accounts struct {
	Name                 string                      `yaml:"name"`
	AuthTenantID         string                      `yaml:"auth_tenant_id"`
	GCPolicies           []keppel.GCPolicy           `yaml:"gc_policies"`
	RBACPolicies         []keppel.RBACPolicy         `yaml:"rbac_policies"`
	ReplicationPolicy    keppel.ReplicationPolicy    `yaml:"replication"`
	SecurityScanPolicies []keppel.SecurityScanPolicy `yaml:"security_scan_policies"`
	ValidationPolicy     keppel.ValidationPolicy     `yaml:"validation"`
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
			ReplicationPolicy: &cfgAccount.ReplicationPolicy,
			ValidationPolicy: &keppel.ValidationPolicy{
				RequiredLabels: cfgAccount.ValidationPolicy.RequiredLabels,
			},
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

	decoder := yaml.NewDecoder(reader)
	decoder.SetStrict(true)
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
