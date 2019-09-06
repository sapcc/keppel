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

package keppel

import "errors"

//NameClaim describes the operation of claiming an account name.
type NameClaim struct {
	//AccountName is the name that is being claimed.
	AccountName string
	//AuthTenantID identifies the auth tenant that this name is being claimed by.
	AuthTenantID string
	//Authorization is the keppel.Authorization for the user requesting the claim.
	Authorization Authorization
}

//NameClaimDriver is the abstract interface for a strategy that coordinates the
//claiming of account names across Keppel deployments.
type NameClaimDriver interface {
	//Commit returns nil if the claim has been executed successfully and durably,
	//or a human-readable error message if the user or auth tenant is not allowed
	//to claim the account name in question.
	Commit(claim NameClaim) error
	//Check shall return the same value as Commit, but not actually commit the
	//claim.
	Check(claim NameClaim) error
}

var nameClaimDriverFactories = make(map[string]func(AuthDriver, Configuration) (NameClaimDriver, error))

//NewNameClaimDriver creates a new NameClaimDriver using one of the factory
//functions registered with RegisterNameClaimDriver().
func NewNameClaimDriver(name string, authDriver AuthDriver, cfg Configuration) (NameClaimDriver, error) {
	factory := nameClaimDriverFactories[name]
	if factory != nil {
		return factory(authDriver, cfg)
	}
	return nil, errors.New("no such nameClaim driver: " + name)
}

//RegisterNameClaimDriver registers an NameClaimDriver. Call this from func
//init() of the package defining the NameClaimDriver.
//
//Factory implementations should inspect the auth driver to ensure that the
//name claim driver can work with this authentication method, returning
//ErrAuthDriverMismatch otherwise.
func RegisterNameClaimDriver(name string, factory func(AuthDriver, Configuration) (NameClaimDriver, error)) {
	if _, exists := nameClaimDriverFactories[name]; exists {
		panic("attempted to register multiple nameClaim drivers with name = " + name)
	}
	nameClaimDriverFactories[name] = factory
}
