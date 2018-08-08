/*******************************************************************************
*
* Copyright 2018 SAP SE
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

//Package drivers defines the generic interfaces between Keppel and the outside
//world: its authentication and storage providers.
package drivers

import "errors"

var authDriverFactories = make(map[string]func() AuthDriver)
var storageDriverFactories = make(map[string]func() StorageDriver)

//NewAuthDriver creates a new AuthDriver using one of the factory functions
//registered with RegisterAuthDriver().
func NewAuthDriver(name string) (AuthDriver, error) {
	factory := authDriverFactories[name]
	if factory != nil {
		return factory(), nil
	}
	return nil, errors.New("no such auth driver: " + name)
}

//NewStorageDriver creates a new StorageDriver using one of the factory functions
//registered with RegisterStorageDriver().
func NewStorageDriver(name string) (StorageDriver, error) {
	factory := storageDriverFactories[name]
	if factory != nil {
		return factory(), nil
	}
	return nil, errors.New("no such storage driver: " + name)
}

//RegisterAuthDriver registers an AuthDriver. Call this from func init() of the
//package defining the AuthDriver.
func RegisterAuthDriver(name string, factory func() AuthDriver) {
	if _, exists := authDriverFactories[name]; exists {
		panic("attempted to register multiple auth drivers with name = " + name)
	}
	authDriverFactories[name] = factory
}

//RegisterStorageDriver registers an StorageDriver. Call this from func init() of the
//package defining the StorageDriver.
func RegisterStorageDriver(name string, factory func() StorageDriver) {
	if _, exists := storageDriverFactories[name]; exists {
		panic("attempted to register multiple storage drivers with name = " + name)
	}
	storageDriverFactories[name] = factory
}
