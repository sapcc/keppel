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
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/keppel/internal/keppel"
)

func TestManifestsAPI(t *testing.T) {
	h, _, _, _, brm, db := setup(t)

	//setup two test accounts
	mustInsert(t, db, &keppel.Account{
		Name:         "test1",
		AuthTenantID: "tenant1",
	})
	mustInsert(t, db, &keppel.Account{
		Name:         "test2",
		AuthTenantID: "tenant2",
	})

	//setup test repos (`repo1-2` and `repo2-1` only exist to validate that we
	//don't accidentally list manifests from there)
	mustInsert(t, db, &keppel.Repository{
		Name:        "repo1-1",
		AccountName: "test1",
	})
	mustInsert(t, db, &keppel.Repository{
		Name:        "repo1-2",
		AccountName: "test1",
	})
	mustInsert(t, db, &keppel.Repository{
		Name:        "repo2-1",
		AccountName: "test2",
	})

	//test empty GET
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"manifests": []assert.JSONObject{}},
	}.Check(t, h)

	//insert some dummy manifests and tags into each repo
	for repoID := 1; repoID <= 3; repoID++ {
		for idx := 1; idx <= 10; idx++ {
			mustInsert(t, db, &keppel.Manifest{
				RepositoryID: int64(repoID),
				Digest:       deterministicDummyDigest(repoID*10 + idx),
				MediaType:    "application/vnd.docker.distribution.manifest.v2+json",
				SizeBytes:    uint64(1000 * idx),
				PushedAt:     time.Unix(int64(1000*(repoID*10+idx)), 0),
			})
		}
		//one manifest is referenced by two tags, one is referenced by one tag
		mustInsert(t, db, &keppel.Tag{
			RepositoryID: int64(repoID),
			Name:         "first",
			Digest:       deterministicDummyDigest(repoID*10 + 1),
			PushedAt:     time.Unix(20001, 0),
		})
		mustInsert(t, db, &keppel.Tag{
			RepositoryID: int64(repoID),
			Name:         "stillfirst",
			Digest:       deterministicDummyDigest(repoID*10 + 1),
			PushedAt:     time.Unix(20002, 0),
		})
		mustInsert(t, db, &keppel.Tag{
			RepositoryID: int64(repoID),
			Name:         "second",
			Digest:       deterministicDummyDigest(repoID*10 + 2),
			PushedAt:     time.Unix(20003, 0),
		})
	}

	//the results will only include the tags and manifests for `repoID == 1`
	//because we're asking for the repo "test1/repo1-1"
	renderedManifests := make([]assert.JSONObject, 10)
	for idx := 1; idx <= 10; idx++ {
		renderedManifests[idx-1] = assert.JSONObject{
			"digest":     deterministicDummyDigest(10 + idx),
			"media_type": "application/vnd.docker.distribution.manifest.v2+json",
			"size_bytes": uint64(1000 * idx),
			"pushed_at":  int64(1000 * (10 + idx)),
		}
	}
	renderedManifests[0]["tags"] = []assert.JSONObject{
		{"name": "first", "pushed_at": 20001},
		{"name": "stillfirst", "pushed_at": 20002},
	}
	renderedManifests[1]["tags"] = []assert.JSONObject{
		{"name": "second", "pushed_at": 20003},
	}
	sort.Slice(renderedManifests, func(i, j int) bool {
		return renderedManifests[i]["digest"].(string) < renderedManifests[j]["digest"].(string)
	})

	//test GET without pagination
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"manifests": renderedManifests},
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests?limit=10",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"manifests": renderedManifests},
	}.Check(t, h)

	//test GET with pagination
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests?limit=5",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"manifests": renderedManifests[0:5],
			"truncated": true,
		},
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests?limit=5&marker=" + renderedManifests[4]["digest"].(string),
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONObject{"manifests": renderedManifests[5:10]},
	}.Check(t, h)
	for idx := 0; idx < 9; idx++ {
		expectedBody := assert.JSONObject{
			"manifests": []assert.JSONObject{renderedManifests[idx+1]},
		}
		if idx < 8 {
			expectedBody["truncated"] = true
		}
		assert.HTTPRequest{
			Method:       "GET",
			Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests?limit=1&marker=" + renderedManifests[idx]["digest"].(string),
			Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
			ExpectStatus: http.StatusOK,
			ExpectBody:   expectedBody,
		}.Check(t, h)
	}

	//test GET failure cases
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/doesnotexist/repositories/repo1-1/_manifests",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusNotFound,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories/doesnotexist/_manifests",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusNotFound,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests?limit=foo",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusBadRequest,
		ExpectBody:   assert.StringData("strconv.ParseUint: parsing \"foo\": invalid syntax\n"),
	}.Check(t, h)

	//test DELETE happy case
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/before-delete-manifest.sql")
	expectedBackendRequest := []backendRequest{{
		AccountName: "test1",
		Method:      "DELETE",
		Path:        "/v2/test1/repo1-1/manifests/" + deterministicDummyDigest(11),
		Status:      http.StatusAccepted,
	}}
	brm.ExpectActionToMakeBackendRequests(t, expectedBackendRequest, func() {
		assert.HTTPRequest{
			Method:       "DELETE",
			Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests/" + deterministicDummyDigest(11),
			Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
			ExpectStatus: http.StatusNoContent,
		}.Check(t, h)
	})
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/after-delete-manifest.sql")

	//test DELETE failure cases
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test2/repositories/repo2-1/_manifests/" + deterministicDummyDigest(31),
		Header:       map[string]string{"X-Test-Perms": "delete:tenant1,view:tenant1"},
		ExpectStatus: http.StatusNotFound,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-2/_manifests/" + deterministicDummyDigest(21),
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusForbidden,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/doesnotexist/repositories/repo1-2/_manifests/" + deterministicDummyDigest(11),
		Header:       map[string]string{"X-Test-Perms": "delete:tenant1,view:tenant1"},
		ExpectStatus: http.StatusNotFound,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1/repositories/doesnotexist/_manifests/" + deterministicDummyDigest(11),
		Header:       map[string]string{"X-Test-Perms": "delete:tenant1,view:tenant1"},
		ExpectStatus: http.StatusNotFound,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests/" + deterministicDummyDigest(11),
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
		ExpectStatus: http.StatusNotFound,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests/second",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
		ExpectStatus: http.StatusNotFound,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests/sha256:12345",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
		ExpectStatus: http.StatusNotFound,
	}.Check(t, h)
}
