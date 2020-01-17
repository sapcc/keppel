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

package tasks

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/jarcoal/httpmock"
	"github.com/sapcc/go-bits/assert"
	authapi "github.com/sapcc/keppel/internal/api/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestIssueNewPasswordForPeer(t *testing.T) {
	cfg, db := test.Setup(t)

	//setup an auth API on our side
	ad, err := keppel.NewAuthDriver("unittest")
	if err != nil {
		t.Fatal(err.Error())
	}
	r := mux.NewRouter()
	authapi.NewAPI(cfg, ad, db).AddTo(r)

	//setup a peer
	err = db.Insert(&keppel.Peer{HostName: "peer.example.org"})
	if err != nil {
		t.Fatal(err.Error())
	}

	//setup a mock for the peer that just swallows any password that we give to it
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	mockPeer := mockPeerReceivingPassword{}
	httpmock.RegisterResponder(
		"POST",
		"https://peer.example.org/keppel/v1/auth/peering",
		mockPeer.ReceivePassword,
	)

	var issuedPasswords []string
	for range []int{0, 1, 2, 3, 4} {
		//test successful issuance of password
		timeBeforeIssue := time.Now()
		err = IssueNewPasswordForPeer(cfg, db, getPeerFromDB(t, db))
		if err != nil {
			t.Error(err.Error())
		}
		for idx, previousPassword := range issuedPasswords {
			if mockPeer.Password == previousPassword {
				t.Errorf("expected IssueNewPasswordForPeer to issue a fresh password, but peer still has password #%d", idx+1)
			}
		}
		issuedPasswords = append(issuedPasswords, mockPeer.Password)

		//check that `last_peered_at` was updated
		peerState := getPeerFromDB(t, db)
		if peerState.LastPeeredAt == nil {
			t.Error("expected peer to have last_peered_at, but got nil")
		} else if peerState.LastPeeredAt.Before(timeBeforeIssue) {
			t.Error("expected IssueNewPasswordForPeer to update last_peered_at, but last_peered_at is still old")
		}

		for idx, password := range issuedPasswords {
			//test that the current password and previous password (if any) can be used to authenticate on our side...
			req := assert.HTTPRequest{
				Method: "GET",
				Path:   "/keppel/v1/auth?service=registry.example.org",
				Header: map[string]string{
					"Authorization": keppel.BuildBasicAuthHeader("replication@peer.example.org", password),
				},
				ExpectStatus: http.StatusOK,
			}
			if idx < len(issuedPasswords)-2 {
				//...but any older passwords will not work
				req.ExpectStatus = http.StatusUnauthorized
			}
			req.Check(t, r)
		}
	}

	//test failing issuance of password
	httpmock.Reset()
	httpmock.RegisterResponder(
		"POST",
		"https://peer.example.org/keppel/v1/auth/peering",
		httpmock.NewStringResponder(http.StatusUnauthorized, "unauthorized"),
	)
	peerBeforeFailedIssue := getPeerFromDB(t, db)
	err = IssueNewPasswordForPeer(cfg, db, getPeerFromDB(t, db))
	if err == nil {
		t.Error("expected IssueNewPasswordForPeer to fail, but got err = nil")
	}

	//a failing issuance should not touch the DB
	assert.DeepEqual(t, "peer state after failed IssueNewPasswordForPeer",
		getPeerFromDB(t, db),
		peerBeforeFailedIssue,
	)
}

func getPeerFromDB(t *testing.T, db *keppel.DB) keppel.Peer {
	t.Helper()
	var peer keppel.Peer
	err := db.SelectOne(&peer, `SELECT * FROM peers WHERE hostname = $1`, "peer.example.org")
	if err != nil {
		t.Fatal(err.Error())
	}
	return peer
}

type mockPeerReceivingPassword struct {
	Password string
}

func (p *mockPeerReceivingPassword) ReceivePassword(r *http.Request) (*http.Response, error) {
	//I would prefer to use the actual implementation of this endpoint, but we
	//only have one DB for the whole testcase, and using it for both ourselves
	//and the peer is asking for trouble.

	var req authapi.PeeringRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&req)
	if err != nil {
		return nil, err
	}

	if req.PeerHostName != "registry.example.org" {
		return nil, errors.New("wrong hostname")
	}
	if req.UserName != "replication@peer.example.org" {
		return nil, errors.New("wrong username")
	}
	if req.Password == "" {
		return nil, errors.New("malformed password")
	}

	p.Password = req.Password
	return httpmock.NewStringResponder(http.StatusNoContent, "")(r)
}
