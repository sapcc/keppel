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

package test

//JwtToken contains the parsed contents of the payload section of a JWT token.
type JwtToken struct {
	Issuer    string      `json:"iss"`
	Subject   string      `json:"sub"`
	Audience  string      `json:"aud"`
	ExpiresAt int64       `json:"exp"`
	NotBefore int64       `json:"nbf"`
	IssuedAt  int64       `json:"iat"`
	TokenID   string      `json:"jti"`
	Access    []JwtAccess `json:"access"`
	//The EmbeddedAuthorization is ignored by this test. It will be exercised
	//indirectly in the registry API tests since the registry API uses attributes
	//from the EmbeddedAuthorization.
	Ignored map[string]interface{} `json:"kea"`
}

//JwtAccess appears in type jwtToken.
type JwtAccess struct {
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	Actions []string `json:"actions"`
}
