// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package peerv1_test

import (
	"net/http"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"

	"github.com/sapcc/keppel/internal/test"
)

func TestMain(m *testing.M) {
	easypg.WithTestDB(m, func() int { return m.Run() })
}

func TestAlternativeAuthSchemes(t *testing.T) {
	s := test.NewSetup(t, test.WithPeerAPI)
	h := s.Handler

	// anonymous auth is never allowed, generates an auth challenge for auth.PeerAPIScope
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

	// Testing other auth schemes is pretty much nonsensical because both regular
	// bearer token auth and Keppel API auth do not even allow obtaining a token
	// for the auth.PeerAPIScope.
}
