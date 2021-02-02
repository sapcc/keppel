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
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/keppel/internal/clair"
	"github.com/sapcc/keppel/internal/keppel"
)

func mustInsert(t *testing.T, db *keppel.DB, obj interface{}) {
	t.Helper()
	err := db.Insert(obj)
	if err != nil {
		t.Fatal(err.Error())
	}
}

func deterministicDummyDigest(counter int) string {
	hash := sha256.Sum256(bytes.Repeat([]byte{1}, counter))
	return "sha256:" + hex.EncodeToString(hash[:])
}

func TestReposAPI(t *testing.T) {
	h, _, _, _, _, db := setup(t)

	//setup two test accounts
	mustInsert(t, db, &keppel.Account{
		Name:         "test1",
		AuthTenantID: "tenant1",
	})
	mustInsert(t, db, &keppel.Account{
		Name:         "test2",
		AuthTenantID: "tenant2",
	})

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
		mustInsert(t, db, &keppel.Repository{
			Name:        fmt.Sprintf("repo1-%d", idx),
			AccountName: "test1",
		})
		mustInsert(t, db, &keppel.Repository{
			Name:        fmt.Sprintf("repo2-%d", idx),
			AccountName: "test2",
		})
	}

	//insert some dummy manifests and tags into one of the repos to check the
	//manifest/tag counting
	for idx := 1; idx <= 10; idx++ {
		digest := deterministicDummyDigest(idx)
		manifestPushedAt := time.Unix(int64(10000+10*idx), 0)
		mustInsert(t, db, &keppel.Manifest{
			RepositoryID:        5, //repo1-3
			Digest:              digest,
			MediaType:           "",
			SizeBytes:           uint64(1000 * idx),
			PushedAt:            manifestPushedAt,
			ValidatedAt:         manifestPushedAt,
			VulnerabilityStatus: clair.PendingVulnerabilityStatus,
		})
		if idx <= 3 {
			mustInsert(t, db, &keppel.Tag{
				RepositoryID: 5, //repo1-3
				Name:         fmt.Sprintf("tag%d", idx),
				Digest:       digest,
				PushedAt:     time.Unix(int64(20000+10*idx), 0),
			})
		}
	}

	//test GET without pagination
	renderedRepos := []assert.JSONObject{
		{"name": "repo1-1", "manifest_count": 0, "tag_count": 0},
		{"name": "repo1-2", "manifest_count": 0, "tag_count": 0},
		{"name": "repo1-3", "manifest_count": 10, "tag_count": 3, "size_bytes": 55000, "pushed_at": 20030},
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

	//test GET with pagination
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
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories?limit=3&marker=repo1-5",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"repositories": []assert.JSONObject{}},
	}.Check(t, h)

	//test GET failure cases
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/doesnotexist/repositories",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusNotFound,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories?limit=foo",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusBadRequest,
		ExpectBody:   assert.StringData("strconv.ParseUint: parsing \"foo\": invalid syntax\n"),
	}.Check(t, h)

	//test DELETE happy case
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/before-delete-repo.sql")
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-1",
		Header:       map[string]string{"X-Test-Perms": "delete:tenant1,view:tenant1"},
		ExpectStatus: http.StatusNoContent,
	}.Check(t, h)
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/after-delete-repo.sql")

	//test DELETE failure cases
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test2/repositories/repo2-1",
		Header:       map[string]string{"X-Test-Perms": "delete:tenant1,view:tenant1"},
		ExpectStatus: http.StatusNotFound,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-2",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusForbidden,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/doesnotexist/repositories/repo1-2",
		Header:       map[string]string{"X-Test-Perms": "delete:tenant1,view:tenant1"},
		ExpectStatus: http.StatusNotFound,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1/repositories/doesnotexist",
		Header:       map[string]string{"X-Test-Perms": "delete:tenant1,view:tenant1"},
		ExpectStatus: http.StatusNotFound,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-3",
		Header:       map[string]string{"X-Test-Perms": "delete:tenant1,view:tenant1"},
		ExpectStatus: http.StatusConflict,
		ExpectBody:   assert.StringData("cannot delete repository while there are still manifests in it\n"),
	}.Check(t, h)
}
