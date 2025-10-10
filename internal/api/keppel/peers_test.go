// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppelv1_test

import (
	"net/http"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestPeersAPI(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler

	// check empty response when there are no peers in the DB
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/peers",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"peers": []any{}},
	}.Check(t, h)

	// add some peers
	expectedPeers := []assert.JSONObject{
		{"hostname": "keppel.example.com"},
		{"hostname": "keppel.example.org"},
	}
	for _, peer := range expectedPeers {
		must.SucceedT(t, s.DB.Insert(&models.Peer{HostName: peer["hostname"].(string)}))
	}

	// check non-empty response
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/peers",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"peers": expectedPeers},
	}.Check(t, h)
}
