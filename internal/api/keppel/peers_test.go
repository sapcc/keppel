// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppelv1_test

import (
	"net/http"
	"testing"

	"github.com/majewsky/gg/jsonmatch"
	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestPeersAPI(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	ctx := t.Context()

	// check empty response when there are no peers in the DB
	s.RespondTo(ctx, "GET /keppel/v1/peers", withPerms("view:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{"peers": []any{}})

	// add some peers
	expectedPeers := []jsonmatch.Object{
		{"hostname": "keppel.example.com"},
		{"hostname": "keppel.example.org"},
	}
	for _, peer := range expectedPeers {
		must.SucceedT(t, s.DB.Insert(&models.Peer{HostName: peer["hostname"].(string)}))
	}

	// check non-empty response
	s.RespondTo(ctx, "GET /keppel/v1/peers", withPerms("view:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{"peers": expectedPeers})
}
