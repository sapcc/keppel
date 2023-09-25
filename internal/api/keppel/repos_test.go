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

package keppelv1_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
	"github.com/sapcc/keppel/internal/trivy"
)

func mustInsert(t *testing.T, db *keppel.DB, obj interface{}) {
	t.Helper()
	err := db.Insert(obj)
	if err != nil {
		t.Fatal(err.Error())
	}
}

func mustDo(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err.Error())
	}
}

func mustExec(t *testing.T, db *keppel.DB, query string, args ...interface{}) {
	t.Helper()
	_, err := db.Exec(query, args...)
	if err != nil {
		t.Fatal(err.Error())
	}
}

func TestReposAPI(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI)
	h := s.Handler

	//setup two test accounts
	mustInsert(t, s.DB, &keppel.Account{
		Name:                     "test1",
		AuthTenantID:             "tenant1",
		GCPoliciesJSON:           "[]",
		SecurityScanPoliciesJSON: "[]",
	})
	mustInsert(t, s.DB, &keppel.Account{
		Name:                     "test2",
		AuthTenantID:             "tenant2",
		GCPoliciesJSON:           "[]",
		SecurityScanPoliciesJSON: "[]",
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
		mustInsert(t, s.DB, &keppel.Repository{
			Name:        fmt.Sprintf("repo1-%d", idx),
			AccountName: "test1",
		})
		mustInsert(t, s.DB, &keppel.Repository{
			Name:        fmt.Sprintf("repo2-%d", idx),
			AccountName: "test2",
		})
	}

	//insert some dummy blobs and blob mounts into one of the repos to check the
	//blob size statistics
	filledRepo := keppel.Repository{ID: 5} //repo1-3
	for idx := 1; idx <= 10; idx++ {
		dummyDigest := test.DeterministicDummyDigest(1000 + idx)
		blobPushedAt := time.Unix(int64(1000+10*idx), 0)
		blob := keppel.Blob{
			AccountName: "test1",
			Digest:      dummyDigest,
			SizeBytes:   uint64(2000 * idx),
			PushedAt:    blobPushedAt,
			ValidatedAt: blobPushedAt,
		}
		mustInsert(t, s.DB, &blob)
		err := keppel.MountBlobIntoRepo(s.DB, blob, filledRepo)
		if err != nil {
			t.Fatal(err.Error())
		}
	}

	//insert some dummy manifests and tags into one of the repos to check the
	//manifest/tag counting
	for idx := 1; idx <= 10; idx++ {
		dummyDigest := test.DeterministicDummyDigest(idx)
		manifestPushedAt := time.Unix(int64(10000+10*idx), 0)
		mustInsert(t, s.DB, &keppel.Manifest{
			RepositoryID: filledRepo.ID,
			Digest:       dummyDigest,
			MediaType:    "",
			SizeBytes:    uint64(1000 * idx),
			PushedAt:     manifestPushedAt,
			ValidatedAt:  manifestPushedAt,
		})
		mustInsert(t, s.DB, &keppel.TrivySecurityInfo{
			RepositoryID:        filledRepo.ID,
			Digest:              dummyDigest,
			VulnerabilityStatus: trivy.PendingVulnerabilityStatus,
			NextCheckAt:         time.Unix(0, 0),
		})
		if idx <= 3 {
			mustInsert(t, s.DB, &keppel.Tag{
				RepositoryID: 5, //repo1-3
				Name:         fmt.Sprintf("tag%d", idx),
				Digest:       dummyDigest,
				PushedAt:     time.Unix(int64(20000+10*idx), 0),
			})
		}
	}

	//test GET without pagination
	renderedRepos := []assert.JSONObject{
		{"name": "repo1-1", "manifest_count": 0, "tag_count": 0},
		{"name": "repo1-2", "manifest_count": 0, "tag_count": 0},
		{"name": "repo1-3", "manifest_count": 10, "tag_count": 3, "size_bytes": 110000, "pushed_at": 20030},
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
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("no permission for keppel_account:doesnotexist:view\n"),
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories?limit=foo",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusBadRequest,
		ExpectBody:   assert.StringData("strconv.ParseUint: parsing \"foo\": invalid syntax\n"),
	}.Check(t, h)

	//test DELETE happy case
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/before-delete-repo.sql")
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-1",
		Header:       map[string]string{"X-Test-Perms": "delete:tenant1,view:tenant1"},
		ExpectStatus: http.StatusNoContent,
	}.Check(t, h)
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/after-delete-repo.sql")

	//test DELETE failure cases
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test2/repositories/repo2-1",
		Header:       map[string]string{"X-Test-Perms": "delete:tenant1,view:tenant1"},
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("no permission for repository:test2/repo2-1:delete\n"),
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
		ExpectStatus: http.StatusForbidden,
		ExpectBody:   assert.StringData("no permission for repository:doesnotexist/repo1-2:delete\n"),
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
