/*******************************************************************************
*
* Copyright 2024 SAP SE
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
func (a *AccountManagementDriver) PluginTypeID() string { return "trivial" } //nolint:goconst

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
