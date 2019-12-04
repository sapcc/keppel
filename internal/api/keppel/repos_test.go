/******************************************************************************
*
*  Copyright 2019 SAP SE
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
	"fmt"
	"net/http"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/keppel/internal/keppel"
)

func TestReposAPI(t *testing.T) {
	h, _, _, _, db := setup(t)

	//setup two test accounts
	for idx := 1; idx <= 2; idx++ {
		err := db.Insert(&keppel.Account{
			Name:         fmt.Sprintf("test%d", idx),
			AuthTenantID: fmt.Sprintf("tenant%d", idx),
		})
		if err != nil {
			t.Fatal(err.Error())
		}
	}

	//test empty result
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"repositories": []assert.JSONObject{},
		},
	}.Check(t, h)

	//setup five repos in each account (the `test2` account only exists to
	//validate that we don't accidentally list its repos as well)
	for idx := 1; idx <= 5; idx++ {
		err := db.Insert(&keppel.Repository{
			Name:        fmt.Sprintf("repo1-%d", idx),
			AccountName: "test1",
		})
		if err != nil {
			t.Fatal(err.Error())
		}
		err = db.Insert(&keppel.Repository{
			Name:        fmt.Sprintf("repo2-%d", idx),
			AccountName: "test2",
		})
		if err != nil {
			t.Fatal(err.Error())
		}
	}

	//test result without pagination
	renderedRepos := []assert.JSONObject{
		{"name": "repo1-1", "manifest_count": 0, "tag_count": 0},
		{"name": "repo1-2", "manifest_count": 0, "tag_count": 0},
		{"name": "repo1-3", "manifest_count": 0, "tag_count": 0},
		{"name": "repo1-4", "manifest_count": 0, "tag_count": 0},
		{"name": "repo1-5", "manifest_count": 0, "tag_count": 0},
	}
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"repositories": renderedRepos},
	}.Check(t, h)

	//TODO test result with pagination
	//TODO test result with non-zero manifest/tag counts
}
