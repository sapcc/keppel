/*******************************************************************************
*
* Copyright 2021 SAP SE
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

package auth

import "github.com/sapcc/keppel/internal/keppel"

//Authorization describes the access rights of a particular user session, i.e.
//in the scope of an individual API request.
type Authorization struct {
	//UserIdentity identifies the user that sent the request.
	UserIdentity keppel.UserIdentity
	//ScopeSet identifies the permissions granted to the user for the duration of
	//this request.
	ScopeSet ScopeSet
	//Audience identifies the API endpoint where the user sent the request.
	Audience Audience
}
