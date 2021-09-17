/******************************************************************************
*
*  Copyright 2021 SAP SE
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
	"testing"

	"github.com/sapcc/keppel/internal/keppel"
)

//GetToken obtains a token for use with the Registry V2 API.
//
//`scope` is the token scope, e.g. "repository:test1/foo:pull". `authTenantID`
//is the ID of the auth tenant backing the requested account. `perms` is the
//set of permissions that the requesting user has; the AuthDriver will set up
//corresponding mock permissions for the duration of the token request.
func (s Setup) GetToken(t *testing.T, scope, authTenantID string, perms ...keppel.Permission) string {
	t.Helper()
	return s.AD.getTokenForTest(t, s.Handler, s.Config.APIPublicURL.Host, scope, authTenantID, perms)
}

//GetAnycastToken is like GetToken, but instead returns a token for the anycast
//endpoint.
func (s Setup) GetAnycastToken(t *testing.T, scope, authTenantID string, perms ...keppel.Permission) string {
	t.Helper()
	return s.AD.getTokenForTest(t, s.Handler, "registry-global.example.org", scope, authTenantID, perms)
}
