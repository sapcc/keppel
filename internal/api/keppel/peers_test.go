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

package keppelv1

import (
	"net/http"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/keppel/internal/keppel"
)

func TestPeersAPI(t *testing.T) {
	h, _, _, _, _, db, _ := setup(t)

	//check empty response when there are no peers in the DB
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/peers",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"peers": []interface{}{}},
	}.Check(t, h)

	//add some peers
	expectedPeers := []assert.JSONObject{
		{"hostname": "keppel.example.com"},
		{"hostname": "keppel.example.org"},
	}
	for _, peer := range expectedPeers {
		err := db.Insert(&keppel.Peer{HostName: peer["hostname"].(string)})
		if err != nil {
			t.Fatal(err)
		}
	}

	//check non-empty response
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/peers",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"peers": expectedPeers},
	}.Check(t, h)
}
