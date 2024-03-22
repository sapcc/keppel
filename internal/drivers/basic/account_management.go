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
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sapcc/keppel/internal/keppel"

	"gopkg.in/yaml.v2"
)

// AccountManagementDriver is the account management driver "basic".
type AccountManagementDriver struct {
	configPath string
	config     AccountConfig
}

type AccountConfig struct {
	Accounts []Accounts `yaml:"accounts"`
}

type Accounts struct {
	Name                 string                      `yaml:"name"`
	AuthTenantID         string                      `yaml:"auth_tenant_id"`
	GCPolicies           []keppel.GCPolicy           `yaml:"gc_policies"`
	RBACPolicies         []keppel.RBACPolicy         `yaml:"rbac_policies"`
	ReplicationPolicy    keppel.ReplicationPolicy    `yaml:"replication_policy"`
	SecurityScanPolicies []keppel.SecurityScanPolicy `yaml:"security_scan_policies"`
	ValidationPolicy     keppel.ValidationPolicy     `yaml:"validation_policy"`
}

func init() {
	keppel.AccountManagementDriverRegistry.Add(func() keppel.AccountManagementDriver {
		return &AccountManagementDriver{}
	})
}

// PluginTypeID implements the keppel.AccountManagementDriver interface.
func (a *AccountManagementDriver) PluginTypeID() string { return "basic" }

// ConfigureAccount implements the keppel.AccountManagementDriver interface.
func (a *AccountManagementDriver) Init() error {
	a.configPath = os.Getenv("KEPPEL_ACCOUNT_MANAGEMENT_FILE")
	if a.configPath == "" {
		return errors.New("KEPPEL_ACCOUNT_MANAGEMENT_FILE is not set")
	}

	return a.loadConfig()
}

// ConfigureAccount implements the keppel.AccountManagementDriver interface.
func (a *AccountManagementDriver) ConfigureAccount(db *keppel.DB, account keppel.Account) (keppel.Account, error) {
	for _, cfgAccount := range a.config.Accounts {
		if cfgAccount.AuthTenantID != account.AuthTenantID {
			continue
		}

		account.IsManaged = true
		account.RequiredLabels = strings.Join(cfgAccount.ValidationPolicy.RequiredLabels, ",")

		gcPolicyJSON, err := json.Marshal(cfgAccount.GCPolicies)
		if err != nil {
			return keppel.Account{}, fmt.Errorf("gc_policies: %w", err)
		}
		account.GCPoliciesJSON = string(gcPolicyJSON)

		rbacPolicyJSON, err := json.Marshal(cfgAccount.RBACPolicies)
		if err != nil {
			return keppel.Account{}, fmt.Errorf("rbac_policies: %w", err)
		}
		account.RBACPoliciesJSON = string(rbacPolicyJSON)

		securityScanPoliciesJSON, err := json.Marshal(cfgAccount.SecurityScanPolicies)
		if err != nil {
			return keppel.Account{}, fmt.Errorf("security_scan_policies: %w", err)
		}
		account.SecurityScanPoliciesJSON = string(securityScanPoliciesJSON)

		_, err = cfgAccount.ReplicationPolicy.ApplyToAccount(db, &account)
		if err != nil {
			return keppel.Account{}, fmt.Errorf("replication_policy: %w", err)
		}

		return account, nil
	}

	// we didn't find the account, delete it
	return keppel.Account{}, nil
}

func (a *AccountManagementDriver) ManagedAccountNames() ([]string, error) {
	err := a.loadConfig()
	if err != nil {
		return nil, err
	}

	var accounts []string
	for _, account := range a.config.Accounts {
		accounts = append(accounts, account.Name)
	}

	return accounts, nil
}

func (a *AccountManagementDriver) loadConfig() error {
	reader, err := os.Open(a.configPath)
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

	a.config = config
	return nil
}
