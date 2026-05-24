// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/sapcc/go-bits/httptest"
	"github.com/sapcc/go-bits/must"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// MustUpload uploads the blob via the Registry V2 API.
//
// `h` must serve the Registry V2 API.
// `token` must be a Bearer token capable of pushing into the specified repo.
func (b Bytes) MustUpload(t testing.TB, s Setup, repo models.Repository) models.Blob {
	t.Helper()

	tokenHeaders := s.GetTokenHeaders(t, fmt.Sprintf("repository:%s:pull,push", repo.FullName()))

	// create blob with a monolithic upload
	s.RespondTo(t.Context(), fmt.Sprintf("POST /v2/%s/blobs/uploads/?digest=%s", repo.FullName(), b.Digest),
		httptest.WithHeaders(tokenHeaders),
		httptest.WithHeader("Content-Length", strconv.Itoa(len(b.Contents))),
		httptest.WithHeader("Content-Type", b.MediaType),
		httptest.WithBody(bytes.NewReader(b.Contents)),
	).ExpectStatus(t, http.StatusCreated)
	if t.Failed() {
		t.FailNow()
	}

	// validate uploaded blob (FindBlobByRepository does not work here because we
	// are usually given a Repository instance that does not have the ID field
	// filled)
	blob := must.ReturnT(keppel.FindBlobByRepositoryName(s.DB, b.Digest, repo.Name, repo.AccountName))(t)
	s.ExpectBlobsExistInStorage(t, blob)
	if t.Failed() {
		t.FailNow()
	}
	return blob
}

var checkBlobExistsQuery = sqlext.SimplifyWhitespace(`
	SELECT COUNT(*) FROM blobs WHERE account_name = $1 AND digest = $2
`)

// MustUpload uploads the image via the Registry V2 API. This also
// uploads all referenced blobs that do not exist in the DB yet.
//
// `tagName` may be empty if the image is to be uploaded without tagging.
func (i Image) MustUpload(t testing.TB, s Setup, repo models.Repository, tagName string) models.Manifest {
	t.Helper()

	// upload missing blobs
	for _, blob := range append(i.Layers, i.Config) {
		count := must.ReturnT(s.DB.SelectInt(checkBlobExistsQuery, repo.AccountName, blob.Digest.String()))(t)
		if count == 0 {
			blob.MustUpload(t, s, repo)
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	// upload manifest
	ref := i.DigestRef()
	if tagName != "" {
		ref = models.ManifestReference{Tag: tagName}
	}
	urlPath := fmt.Sprintf("/v2/%s/manifests/%s", repo.FullName(), ref)
	tokenHeaders := s.GetTokenHeaders(t, fmt.Sprintf("repository:%s:pull,push", repo.FullName()))
	s.RespondTo(t.Context(), "PUT "+urlPath,
		httptest.WithHeaders(tokenHeaders),
		httptest.WithHeader("Content-Type", i.Manifest.MediaType),
		httptest.WithBody(bytes.NewReader(i.Manifest.Contents)),
	).ExpectStatus(t, http.StatusCreated)
	if t.Failed() {
		t.FailNow()
	}

	// validate uploaded manifest
	manifest := must.ReturnT(keppel.FindManifestByRepositoryName(s.DB, repo.Name, repo.AccountName, i.Manifest.Digest))(t)
	s.ExpectManifestsExistInStorage(t, repo.Name, manifest)
	if t.Failed() {
		t.FailNow()
	}
	return manifest
}

var checkManifestExistsQuery = sqlext.SimplifyWhitespace(`
	SELECT COUNT(*) FROM manifests m
	  JOIN repos r ON m.repo_id = r.id
	 WHERE r.account_name = $1 AND r.name = $2 AND m.digest = $3
`)

// MustUpload uploads the image list via the Registry V2 API. This
// also uploads all referenced images that do not exist in the DB yet.
//
// `tagName` may be empty if the image is to be uploaded without tagging.
func (l ImageList) MustUpload(t testing.TB, s Setup, repo models.Repository, tagName string) models.Manifest {
	t.Helper()

	// upload missing images
	for _, image := range l.Images {
		count := must.ReturnT(s.DB.SelectInt(checkManifestExistsQuery, repo.AccountName, repo.Name, image.Manifest.Digest))(t)
		if count == 0 {
			image.MustUpload(t, s, repo, "")
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	// upload manifest
	ref := l.DigestRef()
	if tagName != "" {
		ref = models.ManifestReference{Tag: tagName}
	}
	urlPath := fmt.Sprintf("/v2/%s/manifests/%s", repo.FullName(), ref)
	tokenHeaders := s.GetTokenHeaders(t, fmt.Sprintf("repository:%s:pull,push", repo.FullName()))
	s.RespondTo(t.Context(), "PUT "+urlPath,
		httptest.WithHeaders(tokenHeaders),
		httptest.WithHeader("Content-Type", l.Manifest.MediaType),
		httptest.WithBody(bytes.NewReader(l.Manifest.Contents)),
	).ExpectStatus(t, http.StatusCreated)
	if t.Failed() {
		t.FailNow()
	}

	// validate uploaded manifest
	manifest := must.ReturnT(keppel.FindManifestByRepositoryName(s.DB, repo.Name, repo.AccountName, l.Manifest.Digest))(t)
	s.ExpectManifestsExistInStorage(t, repo.Name, manifest)
	if t.Failed() {
		t.FailNow()
	}
	return manifest
}
