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
	"strings"
	"testing"
	"time"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/hermes/pkg/cadf"
	"github.com/sapcc/keppel/internal/clair"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func deterministicDummyVulnStatus(counter int) clair.VulnerabilityStatus {
	if counter%5 == 0 {
		return clair.PendingVulnerabilityStatus
	}
	if counter%3 == 0 {
		return clair.HighSeverity
	}
	return clair.CleanSeverity
}

func TestManifestsAPI(t *testing.T) {
	h, _, _, auditor, sd, db, _, claird := setup(t)

	//setup two test accounts
	mustInsert(t, db, &keppel.Account{
		Name:           "test1",
		AuthTenantID:   "tenant1",
		GCPoliciesJSON: "[]",
	})
	mustInsert(t, db, &keppel.Account{
		Name:           "test2",
		AuthTenantID:   "tenant2",
		GCPoliciesJSON: "[]",
	})

	//setup test repos (`repo1-2` and `repo2-1` only exist to validate that we
	//don't accidentally list manifests from there)
	repos := []*keppel.Repository{
		{Name: "repo1-1", AccountName: "test1"},
		{Name: "repo1-2", AccountName: "test1"},
		{Name: "repo2-1", AccountName: "test2"},
	}
	for _, repo := range repos {
		mustInsert(t, db, repo)
	}

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
		repo := repos[repoID-1]

		for idx := 1; idx <= 10; idx++ {
			digest := deterministicDummyDigest(repoID*10 + idx)
			sizeBytes := uint64(1000 * idx)
			pushedAt := time.Unix(int64(1000*(repoID*10+idx)), 0)

			dbManifest := keppel.Manifest{
				RepositoryID:        int64(repoID),
				Digest:              digest,
				MediaType:           "application/vnd.docker.distribution.manifest.v2+json",
				SizeBytes:           sizeBytes,
				PushedAt:            pushedAt,
				ValidatedAt:         pushedAt,
				VulnerabilityStatus: deterministicDummyVulnStatus(idx),
				LabelsJSON:          `{"foo":"is there"}`,
			}
			if idx == 1 {
				dbManifest.LastPulledAt = p2time(pushedAt.Add(100 * time.Second))
			}
			mustInsert(t, db, &dbManifest)

			err := sd.WriteManifest(
				keppel.Account{Name: repo.AccountName},
				repo.Name, digest, []byte(strings.Repeat("x", int(sizeBytes))),
			)
			if err != nil {
				t.Fatal(err.Error())
			}
		}
		//one manifest is referenced by two tags, one is referenced by one tag
		mustInsert(t, db, &keppel.Tag{
			RepositoryID: int64(repoID),
			Name:         "first",
			Digest:       deterministicDummyDigest(repoID*10 + 1),
			PushedAt:     time.Unix(20001, 0),
			LastPulledAt: p2time(time.Unix(20101, 0)),
		})
		mustInsert(t, db, &keppel.Tag{
			RepositoryID: int64(repoID),
			Name:         "stillfirst",
			Digest:       deterministicDummyDigest(repoID*10 + 1),
			PushedAt:     time.Unix(20002, 0),
			LastPulledAt: nil,
		})
		mustInsert(t, db, &keppel.Tag{
			RepositoryID: int64(repoID),
			Name:         "second",
			Digest:       deterministicDummyDigest(repoID*10 + 2),
			PushedAt:     time.Unix(20003, 0),
			LastPulledAt: nil,
		})
	}

	//the results will only include the tags and manifests for `repoID == 1`
	//because we're asking for the repo "test1/repo1-1"
	renderedManifests := make([]assert.JSONObject, 10)
	for idx := 1; idx <= 10; idx++ {
		renderedManifests[idx-1] = assert.JSONObject{
			"digest":               deterministicDummyDigest(10 + idx),
			"media_type":           "application/vnd.docker.distribution.manifest.v2+json",
			"size_bytes":           uint64(1000 * idx),
			"pushed_at":            int64(1000 * (10 + idx)),
			"last_pulled_at":       nil,
			"labels":               assert.JSONObject{"foo": "is there"},
			"vulnerability_status": string(deterministicDummyVulnStatus(idx)),
		}
	}
	renderedManifests[0]["last_pulled_at"] = 11100
	renderedManifests[0]["tags"] = []assert.JSONObject{
		{"name": "first", "pushed_at": 20001, "last_pulled_at": 20101},
		{"name": "stillfirst", "pushed_at": 20002, "last_pulled_at": nil},
	}
	renderedManifests[1]["tags"] = []assert.JSONObject{
		{"name": "second", "pushed_at": 20003, "last_pulled_at": nil},
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

	//test DELETE manifest happy case
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/before-delete-manifest.sql")
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests/" + deterministicDummyDigest(11),
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
		ExpectStatus: http.StatusNoContent,
	}.Check(t, h)
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/after-delete-manifest.sql")

	auditor.ExpectEvents(t, cadf.Event{
		RequestPath: "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests/" + deterministicDummyDigest(11),
		Action:      "delete",
		Outcome:     "success",
		Reason:      test.CADFReasonOK,
		Target: cadf.Resource{
			TypeURI:   "docker-registry/account/repository/manifest",
			Name:      "test1/repo1-1@" + deterministicDummyDigest(11),
			ID:        deterministicDummyDigest(11),
			ProjectID: "tenant1",
		},
	})

	//test DELETE tag happy case
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-2/_tags/stillfirst",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
		ExpectStatus: http.StatusNoContent,
	}.Check(t, h)
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/after-delete-tag.sql")

	auditor.ExpectEvents(t, cadf.Event{
		RequestPath: "/keppel/v1/accounts/test1/repositories/repo1-2/_tags/stillfirst",
		Action:      "delete",
		Outcome:     "success",
		Reason:      test.CADFReasonOK,
		Target: cadf.Resource{
			TypeURI:   "docker-registry/account/repository/tag",
			Name:      "test1/repo1-2:stillfirst",
			ID:        deterministicDummyDigest(21),
			ProjectID: "tenant1",
		},
	})

	//test DELETE manifest failure cases
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
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests/second", //this endpoint only works with digests
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
		ExpectStatus: http.StatusNotFound,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests/sha256:12345",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1,delete:tenant1"},
		ExpectStatus: http.StatusNotFound,
	}.Check(t, h)

	//test DELETE tag failure cases
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-2/_tags/first",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusForbidden,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test2/repositories/repo2-1/_tags/" + deterministicDummyDigest(31), //this endpoint only works with tags
		Header:       map[string]string{"X-Test-Perms": "delete:tenant2,view:tenant2"},
		ExpectStatus: http.StatusNotFound,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test2/repositories/doesnotexist/_tags/first",
		Header:       map[string]string{"X-Test-Perms": "delete:tenant2,view:tenant2"},
		ExpectStatus: http.StatusNotFound,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test2/repositories/repo2-1/_tags/doesnotexist",
		Header:       map[string]string{"X-Test-Perms": "delete:tenant2,view:tenant2"},
		ExpectStatus: http.StatusNotFound,
	}.Check(t, h)

	//test GET vulnerability report failure cases
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests/" + deterministicDummyDigest(11) + "/vulnerability_report",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusNotFound, //this manifest was deleted above
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests/" + deterministicDummyDigest(12) + "/vulnerability_report",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusMethodNotAllowed, //manifest cannot have vulnerability report because it does not have manifest-blob refs
	}.Check(t, h)

	//setup a dummy blob that's correctly mounted and linked to our test manifest
	//so that the vulnerability report can actually be shown
	dummyBlob := keppel.Blob{
		AccountName: "test1",
		Digest:      deterministicDummyDigest(101),
	}
	mustInsert(t, db, &dummyBlob)
	err := keppel.MountBlobIntoRepo(db, dummyBlob, *repos[0])
	if err != nil {
		t.Fatal(err.Error())
	}
	_, err = db.Exec(
		`INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES ($1, $2, $3)`,
		repos[0].ID, deterministicDummyDigest(12), dummyBlob.ID,
	)
	if err != nil {
		t.Fatal(err.Error())
	}

	//configure our ClairDouble to present a vulnerability report for our test manifest
	claird.WasIndexSubmitted[deterministicDummyDigest(12)] = true
	claird.ReportFixtures[deterministicDummyDigest(12)] = "fixtures/clair-report-vulnerable.json"
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-1/_manifests/" + deterministicDummyDigest(12) + "/vulnerability_report",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.JSONFixtureFile("fixtures/clair-report-vulnerable.json"),
	}.Check(t, h)
}

func p2time(x time.Time) *time.Time {
	return &x
}
