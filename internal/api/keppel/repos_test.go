// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppelv1_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/majewsky/gg/jsonmatch"
	. "github.com/majewsky/gg/option"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestReposAPI(t *testing.T) {
	s := test.NewSetup(t, test.WithKeppelAPI, test.WithQuotas,
		test.WithAccount(models.Account{Name: "test1", AuthTenantID: "tenant1"}),
		test.WithAccount(models.Account{Name: "test2", AuthTenantID: "tenant2"}))
	h := s.Handler
	ctx := t.Context()

	// test empty result
	h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories", withPerms("view:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"repositories": []jsonmatch.Object{},
		})

	// setup five repos in each account (the `test2` account only exists to
	// validate that we don't accidentally list its repos as well)
	for idx := 1; idx <= 5; idx++ {
		must.SucceedT(t, s.DB.Insert(&models.Repository{
			Name:        models.RepositoryName(fmt.Sprintf("repo1-%d", idx)),
			AccountName: "test1",
		}))
		must.SucceedT(t, s.DB.Insert(&models.Repository{
			Name:        models.RepositoryName(fmt.Sprintf("repo2-%d", idx)),
			AccountName: "test2",
		}))
	}

	// insert some dummy blobs and blob mounts into one of the repos to check the blob size statistics
	filledRepo := models.ReducedRepository{ID: 5} // repo1-3
	for idx := 1; idx <= 10; idx++ {
		dummyDigest := test.DeterministicDummyDigest(1000 + idx)
		blobPushedAt := time.Unix(int64(1000+10*idx), 0)
		blob := models.Blob{
			AccountName:      "test1",
			Digest:           dummyDigest,
			SizeBytes:        uint64(2000 * idx),
			PushedAt:         blobPushedAt,
			NextValidationAt: blobPushedAt.Add(models.BlobValidationInterval),
		}
		must.SucceedT(t, s.DB.Insert(&blob))
		must.SucceedT(t, keppel.MountBlobIntoRepo(s.DB, blob, filledRepo))
	}

	// insert some dummy manifests and tags into one of the repos to check the manifest/tag counting
	for idx := 1; idx <= 8; idx++ {
		dummyDigest := test.DeterministicDummyDigest(idx)
		manifestPushedAt := time.Unix(int64(10000+10*idx), 0)
		must.SucceedT(t, s.DB.Insert(&models.Manifest{
			RepositoryID:     filledRepo.ID,
			Digest:           dummyDigest,
			MediaType:        "",
			SizeBytes:        uint64(1000 * idx),
			PushedAt:         manifestPushedAt,
			NextValidationAt: manifestPushedAt.Add(models.ManifestValidationInterval),
		}))
		must.SucceedT(t, s.SD.WriteManifest(s.Ctx, models.ReducedAccount{Name: "test1"}, "repo1-3", dummyDigest, []byte("data")))
		must.SucceedT(t, s.DB.Insert(&models.TrivySecurityInfo{
			RepositoryID:        filledRepo.ID,
			Digest:              dummyDigest,
			VulnerabilityStatus: models.PendingVulnerabilityStatus,
			NextCheckAt:         Some(time.Unix(0, 0)),
		}))
		if idx <= 3 {
			must.SucceedT(t, s.DB.Insert(&models.Tag{
				RepositoryID: 5, // repo1-3
				Name:         fmt.Sprintf("tag%d", idx),
				Digest:       dummyDigest,
				PushedAt:     time.Unix(int64(20000+10*idx), 0),
			}))
		}
	}

	// Also have a SubjectDigest to test with ...
	repoRef := models.Repository{AccountName: "test1", Name: "repo1-3"}
	subjectDigest := test.DeterministicDummyDigest(9)
	subjectManifest := test.GenerateOCIImage(test.OCIArgs{
		ConfigMediaType: imgspecv1.MediaTypeImageManifest,
		SubjectDigest:   subjectDigest,
	})
	subjectManifest.MustUpload(t, s, repoRef, strings.ReplaceAll(subjectDigest.String(), ":", "-"))

	// ... and a manifest list
	image := test.GenerateImage(test.GenerateExampleLayer(10))
	image.MustUpload(t, s, repoRef, "")
	imageList := test.GenerateImageList(image)
	imageList.MustUpload(t, s, repoRef, "")

	// test GET without pagination
	renderedRepos := []jsonmatch.Object{
		{"name": "repo1-1", "manifest_count": 0, "tag_count": 0},
		{"name": "repo1-2", "manifest_count": 0, "tag_count": 0},
		{"name": "repo1-3", "manifest_count": 11, "tag_count": 4, "size_bytes": 1160180, "pushed_at": 20030},
		{"name": "repo1-4", "manifest_count": 0, "tag_count": 0},
		{"name": "repo1-5", "manifest_count": 0, "tag_count": 0},
	}
	h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories", withPerms("view:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{"repositories": renderedRepos})
	h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories?limit=5", withPerms("view:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{"repositories": renderedRepos})

	// test GET with pagination
	h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories?limit=3", withPerms("view:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"repositories": renderedRepos[0:3],
			"truncated":    true,
		})
	h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories?limit=3&marker=repo1-3", withPerms("view:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{"repositories": renderedRepos[3:5]})
	h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories?limit=3&marker=repo1-2", withPerms("view:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{"repositories": renderedRepos[2:5]})
	h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories?limit=3&marker=repo1-5", withPerms("view:tenant1")).
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{"repositories": []jsonmatch.Object{}})

	// test GET failure cases
	h.RespondTo(ctx, "GET /keppel/v1/accounts/doesnotexist/repositories", withPerms("view:tenant1")).
		ExpectText(t, http.StatusForbidden, "no permission for keppel_account:doesnotexist:view\n")
	h.RespondTo(ctx, "GET /keppel/v1/accounts/test1/repositories?limit=foo", withPerms("view:tenant1")).
		ExpectText(t, http.StatusBadRequest, "strconv.ParseUint: parsing \"foo\": invalid syntax\n")

	// test DELETE failure cases
	h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test2/repositories/repo2-1", withPerms("delete:tenant1,view:tenant1")).
		ExpectText(t, http.StatusForbidden, "no permission for repository:test2/repo2-1:delete\n")
	h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test1/repositories/repo1-2", withPerms("view:tenant1")).
		ExpectText(t, http.StatusForbidden, "no permission for repository:test1/repo1-2:delete\n")
	h.RespondTo(ctx, "DELETE /keppel/v1/accounts/doesnotexist/repositories/repo1-2", withPerms("delete:tenant1,view:tenant1")).
		ExpectText(t, http.StatusForbidden, "no permission for repository:doesnotexist/repo1-2:delete\n")
	h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test1/repositories/doesnotexist", withPerms("delete:tenant1,view:tenant1")).
		ExpectText(t, http.StatusNotFound, "repository not found\n")

	// test if tag policy prevents deletion
	deletingTagPolicyJSON := `{"match_repository":".*","block_delete":true}`
	test.MustExec(t, s.DB, `UPDATE accounts SET tag_policies_json = $1`, "["+deletingTagPolicyJSON+"]")
	h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test1/repositories/repo1-3", withPerms("delete:tenant1,view:tenant1")).
		ExpectText(t, http.StatusConflict, "cannot delete manifest because it is protected by tag policy ({\"match_repository\":\".*\",\"block_delete\":true})\n")
	test.MustExec(t, s.DB, `UPDATE accounts SET tag_policies_json = '[]'`)

	// test DELETE happy case
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/before-delete-repo.sql")
	h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test1/repositories/repo1-1", withPerms("delete:tenant1,view:tenant1")).
		ExpectStatus(t, http.StatusNoContent)
	h.RespondTo(ctx, "DELETE /keppel/v1/accounts/test1/repositories/repo1-3", withPerms("delete:tenant1,view:tenant1")).
		ExpectStatus(t, http.StatusNoContent)
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/after-delete-repo.sql")
}
