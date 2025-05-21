// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package authapi_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/respondwith"

	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestPeeringAPI(t *testing.T) {
	test.WithRoundTripper(func(tt *test.RoundTripper) {
		s := test.NewSetup(t)
		h := s.Handler

		// set up peer.example.org as a peer of us, otherwise we will reject peering
		// attempts from that source
		err := s.DB.Insert(&models.Peer{HostName: "peer.example.org"})
		if err != nil {
			t.Fatal(err.Error())
		}

		// upon receiving a peering request, the implementation will attempt to
		// validate the supplied credentials by calling the peer's auth API - this is
		// a mock implementation for this
		expectedAuthHeader := "Basic cmVwbGljYXRpb25AcmVnaXN0cnkuZXhhbXBsZS5vcmc6c3VwZXJzZWNyZXQ="
		expectedQuery := url.Values{}
		expectedQuery.Set("service", "peer.example.org")
		tt.Handlers["peer.example.org"] = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/keppel/v1/auth" {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if r.Method != http.MethodGet {
				http.Error(w, "not allowed", http.StatusMethodNotAllowed)
				return
			}
			if r.Header.Get("Authorization") != expectedAuthHeader {
				http.Error(w, "wrong Authorization header", http.StatusUnauthorized)
				return
			}
			respondwith.JSON(w, http.StatusOK, map[string]string{"token": "dummy"})
		})

		// error cases
		assert.HTTPRequest{
			Method: "POST",
			Path:   "/keppel/v1/auth/peering",
			Body: assert.JSONObject{
				"peer":     "unknown-peer.example.org", // unknown peer
				"username": "replication@registry.example.org",
				"password": "supersecret",
			},
			ExpectStatus: http.StatusBadRequest,
			ExpectBody:   assert.StringData("unknown issuer\n"),
		}.Check(t, h)

		assert.HTTPRequest{
			Method: "POST",
			Path:   "/keppel/v1/auth/peering",
			Body: assert.JSONObject{
				"peer":     "peer.example.org",
				"username": "replication@someone-else.example.org", // wrong username
				"password": "supersecret",
			},
			ExpectStatus: http.StatusBadRequest,
			ExpectBody:   assert.StringData("wrong audience\n"),
		}.Check(t, h)

		assert.HTTPRequest{
			Method: "POST",
			Path:   "/keppel/v1/auth/peering",
			Body: assert.JSONObject{
				"peer":     "peer.example.org",
				"username": "replication@registry.example.org",
				"password": "incorrect", // wrong password
			},
			ExpectStatus: http.StatusUnauthorized,
			ExpectBody:   assert.StringData("could not validate credentials: expected 200 OK, but got 401 Unauthorized\n"),
		}.Check(t, h)

		// error cases should not touch the DB
		easypg.AssertDBContent(t, s.DB.Db, "fixtures/before-peering.sql")

		// success case
		assert.HTTPRequest{
			Method: "POST",
			Path:   "/keppel/v1/auth/peering",
			Body: assert.JSONObject{
				"peer":     "peer.example.org",
				"username": "replication@registry.example.org",
				"password": "supersecret",
			},
			ExpectStatus: http.StatusNoContent,
			ExpectBody:   assert.StringData(""),
		}.Check(t, h)

		// success case should have touched the DB
		easypg.AssertDBContent(t, s.DB.Db, "fixtures/after-peering.sql")
	})
}
