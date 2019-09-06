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

package trivial

import "github.com/sapcc/keppel/internal/keppel"

type nameClaimDriver struct{}

func init() {
	keppel.RegisterNameClaimDriver("trivial", func(_ keppel.AuthDriver, _ keppel.Configuration) (keppel.NameClaimDriver, error) {
		return nameClaimDriver{}, nil
	})
}

//Commit implements the keppel.NameClaimDriver interface.
func (nameClaimDriver) Commit(claim keppel.NameClaim) error {
	return nil
}

//Check implements the keppel.NameClaimDriver interface.
func (nameClaimDriver) Check(claim keppel.NameClaim) error {
	return nil
}
