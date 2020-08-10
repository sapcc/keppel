/******************************************************************************
*
*  Copyright 2020 SAP SE
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

package authapi

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/jarcoal/httpmock"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/keppel/internal/api"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestPeeringAPI(t *testing.T) {
	cfg, db := test.Setup(t)

	//set up peer.example.org as a peer of us, otherwise we will reject peering
	//attempts from that source
	err := db.Insert(&keppel.Peer{HostName: "peer.example.org"})
	if err != nil {
		t.Fatal(err.Error())
	}

	ad, err := keppel.NewAuthDriver("unittest", nil)
	if err != nil {
		t.Fatal(err.Error())
	}
	fd, err := keppel.NewFederationDriver("unittest", ad, cfg)
	if err != nil {
		t.Fatal(err.Error())
	}

	h := api.Compose(NewAPI(cfg, ad, fd, db))

	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	//upon receiving a peering request, the implementation will attempt to
	//validate the supplied credentials by calling the peer's auth API - this is
	//a mock implementation for this
	expectedAuthHeader := "Basic cmVwbGljYXRpb25AcmVnaXN0cnkuZXhhbXBsZS5vcmc6c3VwZXJzZWNyZXQ="
	expectedQuery := url.Values{}
	expectedQuery.Set("service", "peer.example.org")
	httpmock.RegisterResponderWithQuery(
		"GET",
		"https://peer.example.org/keppel/v1/auth",
		expectedQuery,
		func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("Authorization") != expectedAuthHeader {
				return httpmock.NewStringResponder(
					http.StatusUnauthorized, "wrong Authorization header",
				)(req)
			}
			return httpmock.NewJsonResponderOrPanic(
				http.StatusOK,
				map[string]interface{}{"token": "dummy"},
			)(req)
		},
	)

	//error cases
	assert.HTTPRequest{
		Method: "POST",
		Path:   "/keppel/v1/auth/peering",
		Body: assert.JSONObject{
			"peer":     "unknown-peer.example.org", //unknown peer
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
			"username": "replication@someone-else.example.org", //wrong username
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
			"password": "incorrect", //wrong password
		},
		ExpectStatus: http.StatusUnauthorized,
		ExpectBody:   assert.StringData("could not validate credentials: expected 200 OK, but got 401\n"),
	}.Check(t, h)

	//error cases should not touch the DB
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/before-peering.sql")

	//success case
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

	//success case should have touched the DB
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/after-peering.sql")
}
