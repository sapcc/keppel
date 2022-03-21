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

package peerv1_test

import (
	"net/http"
	"testing"

	"github.com/sapcc/go-bits/assert"

	"github.com/sapcc/keppel/internal/test"
)

func TestAlternativeAuthSchemes(t *testing.T) {
	s := test.NewSetup(t, test.WithPeerAPI)
	h := s.Handler

	//anonymous auth is never allowed, generates an auth challenge for auth.PeerAPIScope
	//test anonymous auth: fails without RBAC policy, succeeds with RBAC policy
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/peer/v1/sync-replica/test1/foo",
		Header:       test.AddHeadersForCorrectAuthChallenge(nil),
		ExpectStatus: http.StatusUnauthorized,
		ExpectHeader: map[string]string{
			"Www-Authenticate": `Bearer realm="https://registry.example.org/keppel/v1/auth",service="registry.example.org",scope="keppel_api:peer:access"`,
		},
		ExpectBody: assert.StringData("no bearer token found in request headers\n"),
	}.Check(t, h)

	//Testing other auth schemes is pretty much nonsensical because both regular
	//bearer token auth and Keppel API auth do not even allow obtaining a token
	//for the auth.PeerAPIScope.
}
