/******************************************************************************
*
*  Copyright 2019 SAP SE
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

package test

import (
	"fmt"

	"github.com/sapcc/keppel/internal/keppel"
)

//NameClaimDriver (driver ID "unittest") is a keppel.NameClaimDriver for unit tests.
type NameClaimDriver struct {
	CheckFails  bool
	CommitFails bool
}

func init() {
	keppel.RegisterNameClaimDriver("unittest", func(_ keppel.AuthDriver, _ keppel.Configuration) (keppel.NameClaimDriver, error) {
		return &NameClaimDriver{}, nil
	})
}

//Check implements the keppel.NameClaimDriver interface.
func (d *NameClaimDriver) Check(claim keppel.NameClaim) error {
	if d.CheckFails {
		return fmt.Errorf("cannot assign name %q to auth tenant %q", claim.AccountName, claim.AuthTenantID)
	}
	return nil
}

//Commit implements the keppel.NameClaimDriver interface.
func (d *NameClaimDriver) Commit(claim keppel.NameClaim) error {
	if d.CommitFails {
		return fmt.Errorf("failed to assign name %q to auth tenant %q", claim.AccountName, claim.AuthTenantID)
	}
	return nil
}
