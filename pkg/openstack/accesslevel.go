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

package openstack

import "github.com/sapcc/keppel/pkg/database"

//AccessLevel describes the permissions that a user has in a given scope.
type AccessLevel interface {
	//Whether the user has permissions to read any accounts at all.
	//If this is false, then CanReadAccount will always return false.
	CanViewAccounts() bool
	//Whether the user has permissions to view this account.
	CanViewAccount(database.Account) bool
	//Whether the user has permissions to create/update/delete this account.
	CanChangeAccount(database.Account) bool

	//TODO: CanChangeAccount implies CanPush, and CanViewAccount implies CanPull.
	//We may want to have more granular permissions here.
}
