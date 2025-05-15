// SPDX-FileCopyrightText: 2020 SAP SE
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/httpapi"

	authapi "github.com/sapcc/keppel/internal/api/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestIssueNewPasswordForPeer(t *testing.T) {
	test.WithRoundTripper(func(tt *test.RoundTripper) {
		s := test.NewSetup(t)

		// setup a peer
		mustDo(t, s.DB.Insert(&models.Peer{HostName: "peer.example.org", UseForPullDelegation: true}))
		mustDo(t, s.DB.Insert(&models.Peer{HostName: "peer.invalid.", UseForPullDelegation: false}))

		// setup a mock for the peer that just swallows any password that we give to it
		mockPeer := mockPeerReceivingPassword{}
		tt.Handlers["peer.example.org"] = httpapi.Compose(&mockPeer)

		var issuedPasswords []string
		for range []int{0, 1, 2, 3, 4} {
			// test successful issuance of password
			timeBeforeIssue := time.Now()
			tx, err := s.DB.Begin()
			if err != nil {
				t.Error(err.Error())
			}
			err = IssueNewPasswordForPeer(s.Ctx, s.Config, s.DB, tx, getPeerFromDB(t, s.DB))
			if err != nil {
				t.Error(err.Error())
			}
			for idx, previousPassword := range issuedPasswords {
				if mockPeer.Password == previousPassword {
					t.Errorf("expected IssueNewPasswordForPeer to issue a fresh password, but peer still has password #%d", idx+1)
				}
			}
			issuedPasswords = append(issuedPasswords, mockPeer.Password)

			// check that `last_peered_at` was updated
			peerState := getPeerFromDB(t, s.DB)
			if peerState.LastPeeredAt == nil {
				t.Error("expected peer to have last_peered_at, but got nil")
			} else if peerState.LastPeeredAt.Before(timeBeforeIssue) {
				t.Error("expected IssueNewPasswordForPeer to update last_peered_at, but last_peered_at is still old")
			}

			for idx, password := range issuedPasswords {
				// test that the current password and previous password (if any) can be used to authenticate on our side...
				req := assert.HTTPRequest{
					Method: "GET",
					Path:   "/keppel/v1/auth?service=registry.example.org",
					Header: map[string]string{
						"Authorization": keppel.BuildBasicAuthHeader("replication@peer.example.org", password),
					},
					ExpectStatus: http.StatusOK,
				}
				if idx < len(issuedPasswords)-2 {
					// ...but any older passwords will not work
					req.ExpectStatus = http.StatusUnauthorized
				}
				req.Check(t, s.Handler)
			}
		}

		// test failing issuance of password
		tt.Handlers["peer.example.org"] = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		})
		peerBeforeFailedIssue := getPeerFromDB(t, s.DB)
		tx, err := s.DB.Begin()
		if err != nil {
			t.Fatal(err.Error())
		}
		err = IssueNewPasswordForPeer(s.Ctx, s.Config, s.DB, tx, getPeerFromDB(t, s.DB))
		if err == nil {
			t.Error("expected IssueNewPasswordForPeer to fail, but got err = nil")
		}

		// a failing issuance should not touch the DB
		assert.DeepEqual(t, "peer state after failed IssueNewPasswordForPeer",
			getPeerFromDB(t, s.DB),
			peerBeforeFailedIssue,
		)
	})
}

func getPeerFromDB(t *testing.T, db *keppel.DB) models.Peer {
	t.Helper()
	var peer models.Peer
	err := db.SelectOne(&peer, `SELECT * FROM peers WHERE use_for_pull_delegation`)
	if err != nil {
		t.Fatal(err.Error())
	}
	return peer
}

type mockPeerReceivingPassword struct {
	Password string
}

// AddTo implements the api.API interface.
func (p *mockPeerReceivingPassword) AddTo(r *mux.Router) {
	r.Methods("POST").Path("/keppel/v1/auth/peering").HandlerFunc(p.handleReceivePassword)
}

func (p *mockPeerReceivingPassword) handleReceivePassword(w http.ResponseWriter, r *http.Request) {
	httpapi.SkipRequestLog(r)
	httpapi.IdentifyEndpoint(r, "/keppel/v1/auth/peering")

	// I would prefer to use the actual implementation of this endpoint, but we
	// only have one DB for the whole testcase, and using it for both ourselves
	// and the peer is asking for trouble.

	var req authapi.PeeringRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	if req.PeerHostName != "registry.example.org" {
		http.Error(w, "wrong hostname", http.StatusUnprocessableEntity)
		return
	}
	if req.UserName != "replication@peer.example.org" {
		http.Error(w, "wrong username", http.StatusUnprocessableEntity)
		return
	}
	if req.Password == "" {
		http.Error(w, "malformed password", http.StatusUnprocessableEntity)
		return
	}

	p.Password = req.Password
	w.WriteHeader(http.StatusNoContent)
}
