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

import "github.com/sapcc/keppel/internal/keppel"

func init() {
	keppel.RegisterStorageDriver("unittest", func(_ keppel.AuthDriver, _ keppel.Configuration) (keppel.StorageDriver, error) {
		return &StorageDriver{}, nil
	})
}

//StorageDriver (driver ID "unittest") is a keppel.StorageDriver for unit tests.
type StorageDriver struct{}

//GetEnvironment implements the keppel.StorageDriver interface.
func (d *StorageDriver) GetEnvironment(account keppel.Account) (map[string]string, error) {
	return map[string]string{"REGISTRY_STORAGE": "inmemory"}, nil
}
