// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppelv1_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	. "github.com/majewsky/gg/option"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestReposAPI(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI, test.WithQuotas,
		test.WithAccount(models.Account{Name: "test1", AuthTenantID: "tenant1"}),
		test.WithAccount(models.Account{Name: "test2", AuthTenantID: "tenant2"}))
	h := s.Handler

	// test empty result
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/keppel/v1/accounts/test1/repositories",
		Header:       map[string]string{"X-Test-Perms": "view:tenant1"},
		ExpectStatus: http.StatusOK,
		ExpectBody: assert.JSONObject{
			"repositories": []assert.JSONObject{},
		},
	}.Check(t, h)

	// setup five repos in each account (the `test2` account only exists to
	// validate that we don't accidentally list its repos as well)
	for idx := 1; idx <= 5; idx++ {
		test.MustInsert(t, s.DB, &models.Repository{
			Name:        fmt.Sprintf("repo1-%d", idx),
			AccountName: "test1",
		})
		test.MustInsert(t, s.DB, &models.Repository{
			Name:        fmt.Sprintf("repo2-%d", idx),
			AccountName: "test2",
		})
	}

	// insert some dummy blobs and blob mounts into one of the repos to check the
	// blob size statistics
	filledRepo := models.Repository{ID: 5} // repo1-3
	for idx := 1; idx <= 10; idx++ {
		dummyDigest := test.DeterministicDummyDigest(1000 + idx)
		blobPushedAt := time.Unix(int64(1000+10*idx), 0)
		blob := models.Blob{
			AccountName:      "test1",
			Digest:           dummyDigest,
			SizeBytes:        uint64(2000 * idx), //nolint:gosec // construction guarantees that value is positive
			PushedAt:         blobPushedAt,
			NextValidationAt: blobPushedAt.Add(models.BlobValidationInterval),
		}
		test.MustInsert(t, s.DB, &blob)
		test.MustDo(t, keppel.MountBlobIntoRepo(s.DB, blob, filledRepo))
	}

	// insert some dummy manifests and tags into one of the repos to check the
	// manifest/tag counting
	for idx := 1; idx <= 9; idx++ {
		dummyDigest := test.DeterministicDummyDigest(idx)
		manifestPushedAt := time.Unix(int64(10000+10*idx), 0)
		test.MustInsert(t, s.DB, &models.Manifest{
			RepositoryID:     filledRepo.ID,
			Digest:           dummyDigest,
			MediaType:        "",
			SizeBytes:        uint64(1000 * idx), //nolint:gosec // construction guarantees that value is positive
			PushedAt:         manifestPushedAt,
			NextValidationAt: manifestPushedAt.Add(models.ManifestValidationInterval),
		})
		test.MustDo(t, s.SD.WriteManifest(s.Ctx, models.ReducedAccount{Name: "test1"}, "repo1-3", dummyDigest, []byte("data")))
		test.MustInsert(t, s.DB, &models.TrivySecurityInfo{
			RepositoryID:        filledRepo.ID,
			Digest:              dummyDigest,
			VulnerabilityStatus: models.PendingVulnerabilityStatus,
			NextCheckAt:         Some(time.Unix(0, 0)),
		})
		if idx <= 3 {
			test.MustInsert(t, s.DB, &models.Tag{
				RepositoryID: 5, // repo1-3
				Name:         fmt.Sprintf("tag%d", idx),
				Digest:       dummyDigest,
				PushedAt:     time.Unix(int64(20000+10*idx), 0),
			})
		}
	}

	// also have a SubjectDigest to test with
	subjectDigest := test.DeterministicDummyDigest(9)
	subjectManifest := test.GenerateOCIImage(test.OCIArgs{
		ConfigMediaType: imgspecv1.MediaTypeImageManifest,
		SubjectDigest:   subjectDigest,
	})
	subjectManifest.MustUpload(t, s, models.Repository{AccountName: "test1", Name: "repo1-3"}, strings.ReplaceAll(subjectDigest.String(), ":", "-"))

	// test GET without pagination
	renderedRepos := []assert.JSONObject{
		{"name": "repo1-1", "manifest_count": 0, "tag_count": 0},
		{"name": "repo1-2", "manifest_count": 0, "tag_count": 0},
		{"name": "repo1-3", "manifest_count": 10, "tag_count": 4, "size_bytes": 110004, "pushed_at": 20030},
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

	// test GET with pagination
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

	// test GET failure cases
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

	// test DELETE failure cases
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
		ExpectBody:   assert.StringData("no permission for repository:test1/repo1-2:delete\n"),
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
		ExpectBody:   assert.StringData("repo not found\n"),
	}.Check(t, h)

	// test if tag policy prevents deletion
	deletingTagPolicyJSON := `{"match_repository":".*","block_delete":true}`
	test.MustExec(t, s.DB, `UPDATE accounts SET tag_policies_json = $1`, "["+deletingTagPolicyJSON+"]")
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-3",
		Header:       map[string]string{"X-Test-Perms": "delete:tenant1,view:tenant1"},
		ExpectStatus: http.StatusConflict,
		ExpectBody:   assert.StringData("cannot delete manifest because it is protected by tag policy ({\"match_repository\":\".*\",\"block_delete\":true})\n"),
	}.Check(t, h)
	test.MustExec(t, s.DB, `UPDATE accounts SET tag_policies_json = '[]'`)

	// test DELETE happy case
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/before-delete-repo.sql")
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-1",
		Header:       map[string]string{"X-Test-Perms": "delete:tenant1,view:tenant1"},
		ExpectStatus: http.StatusNoContent,
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "DELETE",
		Path:         "/keppel/v1/accounts/test1/repositories/repo1-3",
		Header:       map[string]string{"X-Test-Perms": "delete:tenant1,view:tenant1"},
		ExpectStatus: http.StatusNoContent,
	}.Check(t, h)
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/after-delete-repo.sql")
}
