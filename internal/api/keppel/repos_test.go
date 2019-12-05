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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"testing"
	"time"

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

	//insert some dummy manifests and tags into one of the repos to check the
	//manifest/tag counting
	for idx := 1; idx <= 10; idx++ {
		hash := sha256.Sum256(bytes.Repeat([]byte{1}, idx))
		digest := "sha256:" + hex.EncodeToString(hash[:])
		err := db.Insert(&keppel.Manifest{
			RepositoryID: 5, //repo1-3
			Digest:       digest,
			MediaType:    "",
			SizeBytes:    uint64(1000 * idx),
			PushedAt:     time.Now(),
		})
		if err != nil {
			t.Fatal(err.Error())
		}
		if idx <= 3 {
			err = db.Insert(&keppel.Tag{
				RepositoryID: 5, //repo1-3
				Name:         fmt.Sprintf("tag%d", idx),
				Digest:       digest,
				PushedAt:     time.Now(),
			})
			if err != nil {
				t.Fatal(err.Error())
			}
		}
	}

	//test result without pagination
	renderedRepos := []assert.JSONObject{
		{"name": "repo1-1", "manifest_count": 0, "tag_count": 0},
		{"name": "repo1-2", "manifest_count": 0, "tag_count": 0},
		{"name": "repo1-3", "manifest_count": 10, "tag_count": 3},
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
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories?limit=5",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"repositories": renderedRepos},
	}.Check(t, h)

	//test result with pagination
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories?limit=3",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"repositories": renderedRepos[0:3],
			"truncated":    true,
		},
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories?limit=3&marker=repo1-3",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"repositories": renderedRepos[3:5]},
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories?limit=3&marker=repo1-2",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"repositories": renderedRepos[2:5]},
	}.Check(t, h)
}
