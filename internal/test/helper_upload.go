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

package test

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

const (
	// VersionHeaderKey is the standard version header name included in all
	// Registry v2 API responses.
	VersionHeaderKey = "Docker-Distribution-Api-Version"
	// VersionHeaderValue is the standard version header value included in all
	// Registry v2 API responses.
	VersionHeaderValue = "registry/2.0"
)

// VersionHeader is the standard version header included in all Registry v2 API
// responses.
var VersionHeader = map[string]string{VersionHeaderKey: VersionHeaderValue}

// MustUpload uploads the blob via the Registry V2 API.
//
// `h` must serve the Registry V2 API.
// `token` must be a Bearer token capable of pushing into the specified repo.
func (b Bytes) MustUpload(t *testing.T, s Setup, repo models.Repository) models.Blob {
	token := s.GetToken(t, fmt.Sprintf("repository:%s:pull,push", repo.FullName()))

	// create blob with a monolithic upload
	assert.HTTPRequest{
		Method: "POST",
		Path:   fmt.Sprintf("/v2/%s/blobs/uploads/?digest=%s", repo.FullName(), b.Digest),
		Header: map[string]string{
			"Authorization":  "Bearer " + token,
			"Content-Length": strconv.Itoa(len(b.Contents)),
			"Content-Type":   b.MediaType,
		},
		Body:         assert.ByteData(b.Contents),
		ExpectStatus: http.StatusCreated,
	}.Check(t, s.Handler) //nolint:bodyclose // only used in testing
	if t.Failed() {
		t.FailNow()
	}

	// validate uploaded blob (FindBlobByRepository does not work here because we
	// are usually given a Repository instance that does not have the ID field
	// filled)
	account := models.Account{Name: repo.AccountName}
	blob, err := keppel.FindBlobByRepositoryName(s.DB, b.Digest, repo.Name, account)
	mustDo(t, err)
	s.ExpectBlobsExistInStorage(t, *blob)
	if t.Failed() {
		t.FailNow()
	}
	return *blob
}

var checkBlobExistsQuery = sqlext.SimplifyWhitespace(`
	SELECT COUNT(*) FROM blobs WHERE account_name = $1 AND digest = $2
`)

// MustUpload uploads the image via the Registry V2 API. This also
// uploads all referenced blobs that do not exist in the DB yet.
//
// `tagName` may be empty if the image is to be uploaded without tagging.
func (i Image) MustUpload(t *testing.T, s Setup, repo models.Repository, tagName string) models.Manifest {
	// upload missing blobs
	for _, blob := range append(i.Layers, i.Config) {
		count, err := s.DB.SelectInt(checkBlobExistsQuery, repo.AccountName, blob.Digest.String())
		if err != nil {
			t.Fatal(err.Error())
		}
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
	token := s.GetToken(t, fmt.Sprintf("repository:%s:pull,push", repo.FullName()))
	assert.HTTPRequest{
		Method: "PUT",
		Path:   urlPath,
		Header: map[string]string{
			"Authorization": "Bearer " + token,
			"Content-Type":  i.Manifest.MediaType,
		},
		Body:         assert.ByteData(i.Manifest.Contents),
		ExpectStatus: http.StatusCreated,
	}.Check(t, s.Handler) //nolint:bodyclose // only used in testing
	if t.Failed() {
		t.FailNow()
	}

	// validate uploaded manifest
	account := models.Account{Name: repo.AccountName}
	manifest, err := keppel.FindManifestByRepositoryName(s.DB, repo.Name, account, i.Manifest.Digest)
	mustDo(t, err)
	s.ExpectManifestsExistInStorage(t, repo.Name, *manifest)
	if t.Failed() {
		t.FailNow()
	}
	return *manifest
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
func (l ImageList) MustUpload(t *testing.T, s Setup, repo models.Repository, tagName string) models.Manifest {
	// upload missing images
	for _, image := range l.Images {
		count, err := s.DB.SelectInt(checkManifestExistsQuery, repo.AccountName, repo.Name, image.Manifest.Digest)
		if err != nil {
			t.Fatal(err.Error())
		}
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
	token := s.GetToken(t, fmt.Sprintf("repository:%s:pull,push", repo.FullName()))
	assert.HTTPRequest{
		Method: "PUT",
		Path:   urlPath,
		Header: map[string]string{
			"Authorization": "Bearer " + token,
			"Content-Type":  l.Manifest.MediaType,
		},
		Body:         assert.ByteData(l.Manifest.Contents),
		ExpectStatus: http.StatusCreated,
	}.Check(t, s.Handler) //nolint:bodyclose // only used in testing
	if t.Failed() {
		t.FailNow()
	}

	// validate uploaded manifest
	account := models.Account{Name: repo.AccountName}
	manifest, err := keppel.FindManifestByRepositoryName(s.DB, repo.Name, account, l.Manifest.Digest)
	mustDo(t, err)
	s.ExpectManifestsExistInStorage(t, repo.Name, *manifest)
	if t.Failed() {
		t.FailNow()
	}
	return *manifest
}
