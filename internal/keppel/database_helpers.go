/*******************************************************************************
*
* Copyright 2018-2020 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package keppel

import (
	"database/sql"
	"errors"

	"github.com/go-gorp/gorp/v3"
	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/models"
)

// FindAccount works similar to db.SelectOne(), but returns nil instead of
// sql.ErrNoRows if no account exists with this name.
func FindAccount(db gorp.SqlExecutor, name models.AccountName) (*models.Account, error) {
	var account models.Account
	err := db.SelectOne(&account, "SELECT * FROM accounts WHERE name = $1", name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &account, err
}

var reducedAccountGetByNameQuery = sqlext.SimplifyWhitespace(`
	SELECT auth_tenant_id, upstream_peer_hostname,
	       external_peer_url, external_peer_username, external_peer_password,
	       platform_filter, required_labels, is_deleting
	  FROM accounts
	 WHERE name = $1
`)

// FindReducedAccount is like FindAccount, but it returns a ReducedAccount instead.
// This can be significantly faster than FindAccount if only the most common stuff is needed.
func FindReducedAccount(db gorp.SqlExecutor, name models.AccountName) (*models.ReducedAccount, error) {
	a := models.ReducedAccount{Name: name}
	err := db.QueryRow(reducedAccountGetByNameQuery, name).Scan(
		&a.AuthTenantID, &a.UpstreamPeerHostName,
		&a.ExternalPeerURL, &a.ExternalPeerUserName, &a.ExternalPeerPassword,
		&a.PlatformFilter, &a.RequiredLabels, &a.IsDeleting,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &a, err
}

var blobGetQueryByRepoName = sqlext.SimplifyWhitespace(`
	SELECT b.*
	  FROM blobs b
	  JOIN blob_mounts bm ON b.id = bm.blob_id
	  JOIN repos r ON bm.repo_id = r.id
	 WHERE b.account_name = $1 AND b.digest = $2
	   AND r.account_name = $1 AND r.name = $3
`)

var blobGetQueryByRepoID = sqlext.SimplifyWhitespace(`
	SELECT b.*
	  FROM blobs b
	  JOIN blob_mounts bm ON b.id = bm.blob_id
	 WHERE b.account_name = $1 AND b.digest = $2 AND bm.repo_id = $3
`)

var blobGetQueryByAccountName = sqlext.SimplifyWhitespace(`
	SELECT * FROM blobs WHERE account_name = $1 AND digest = $2
`)

// FindBlobByRepositoryName is a convenience wrapper around db.SelectOne(). If
// the blob in question does not exist, sql.ErrNoRows is returned.
func FindBlobByRepositoryName(db gorp.SqlExecutor, blobDigest digest.Digest, repoName string, accountName models.AccountName) (*models.Blob, error) {
	var blob models.Blob
	err := db.SelectOne(&blob, blobGetQueryByRepoName, accountName, blobDigest.String(), repoName)
	return &blob, err
}

// FindBlobByRepository is a convenience wrapper around db.SelectOne(). If
// the blob in question does not exist, sql.ErrNoRows is returned.
func FindBlobByRepository(db gorp.SqlExecutor, blobDigest digest.Digest, repo models.Repository) (*models.Blob, error) {
	var blob models.Blob
	err := db.SelectOne(&blob, blobGetQueryByRepoID, repo.AccountName, blobDigest.String(), repo.ID)
	return &blob, err
}

// FindBlobByAccountName is a convenience wrapper around db.SelectOne(). If the
// blob in question does not exist, sql.ErrNoRows is returned.
func FindBlobByAccountName(db gorp.SqlExecutor, blobDigest digest.Digest, accountName models.AccountName) (*models.Blob, error) {
	var blob models.Blob
	err := db.SelectOne(&blob, blobGetQueryByAccountName, accountName, blobDigest.String())
	return &blob, err
}

// MountBlobIntoRepo creates an entry in the blob_mounts database table.
func MountBlobIntoRepo(db gorp.SqlExecutor, blob models.Blob, repo models.Repository) error {
	_, err := db.Exec(
		`INSERT INTO blob_mounts (blob_id, repo_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		blob.ID, repo.ID,
	)
	return err
}

var uploadGetQueryByRepoID = sqlext.SimplifyWhitespace(`
	SELECT u.* FROM uploads u WHERE u.uuid = $1 AND repo_id = $2
`)

// FindUploadByRepository is a convenience wrapper around db.SelectOne(). If
// the upload in question does not exist, sql.ErrNoRows is returned.
func FindUploadByRepository(db gorp.SqlExecutor, uuid string, repo models.Repository) (*models.Upload, error) {
	var upload models.Upload
	err := db.SelectOne(&upload, uploadGetQueryByRepoID, uuid, repo.ID)
	return &upload, err
}

// FindManifest is a convenience wrapper around db.SelectOne(). If the
// manifest in question does not exist, sql.ErrNoRows is returned.
func FindManifest(db gorp.SqlExecutor, repo models.Repository, manifestDigest digest.Digest) (*models.Manifest, error) {
	var manifest models.Manifest
	err := db.SelectOne(&manifest,
		"SELECT * FROM manifests WHERE repo_id = $1 AND digest = $2", repo.ID, manifestDigest)
	return &manifest, err
}

var manifestGetQueryByRepoName = sqlext.SimplifyWhitespace(`
	SELECT m.*
	  FROM manifests m
	  JOIN repos r ON m.repo_id = r.id
	 WHERE r.account_name = $1 AND r.name = $2 AND m.digest = $3
`)

// FindManifestByRepositoryName is a convenience wrapper around db.SelectOne().
// If the manifest in question does not exist, sql.ErrNoRows is returned.
func FindManifestByRepositoryName(db gorp.SqlExecutor, repoName string, accountName models.AccountName, manifestDigest digest.Digest) (*models.Manifest, error) {
	var manifest models.Manifest
	err := db.SelectOne(&manifest, manifestGetQueryByRepoName, accountName, repoName, manifestDigest.String())
	return &manifest, err
}

// FindQuotas works similar to db.SelectOne(), but returns nil instead of
// sql.ErrNoRows if no quota set exists for this auth tenant.
func FindQuotas(db gorp.SqlExecutor, authTenantID string) (*models.Quotas, error) {
	var quotas models.Quotas
	err := db.SelectOne(&quotas,
		"SELECT * FROM quotas WHERE auth_tenant_id = $1", authTenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &quotas, err
}

var manifestUsageQuery = sqlext.SimplifyWhitespace(`
	SELECT COUNT(m.digest)
	  FROM manifests m
	  JOIN repos r ON m.repo_id = r.id
	  JOIN accounts a ON a.name = r.account_name
	 WHERE a.auth_tenant_id = $1
`)

// GetManifestUsage returns how many manifests currently exist in repos in
// accounts connected to this quota set's auth tenant.
func GetManifestUsage(db gorp.SqlExecutor, quotas models.Quotas) (uint64, error) {
	manifestCount, err := db.SelectInt(manifestUsageQuery, quotas.AuthTenantID)
	return AtLeastZero(manifestCount), err
}

// AtLeastZero safely converts int or int64 values (which might come from
// DB.SelectInt() or from IO reads/writes) to uint64 by clamping negative values to 0.
func AtLeastZero[I interface{ int | int64 }](x I) uint64 {
	if x < 0 {
		return 0
	}
	return uint64(x)
}

// FindOrCreateRepository works similar to db.SelectOne(), but autovivifies a
// Repository record when none exists yet.
func FindOrCreateRepository(db gorp.SqlExecutor, name string, accountName models.AccountName) (*models.Repository, error) {
	repo, err := FindRepository(db, name, accountName)
	if errors.Is(err, sql.ErrNoRows) {
		repo = &models.Repository{
			Name:        name,
			AccountName: accountName,
		}
		err = db.Insert(repo)
	}
	return repo, err
}

// FindRepository is a convenience wrapper around db.SelectOne(). If the
// repository in question does not exist, sql.ErrNoRows is returned.
func FindRepository(db gorp.SqlExecutor, name string, accountName models.AccountName) (*models.Repository, error) {
	var repo models.Repository
	err := db.SelectOne(&repo,
		"SELECT * FROM repos WHERE account_name = $1 AND name = $2", accountName, name)
	return &repo, err
}

// FindRepositoryByID is a convenience wrapper around db.SelectOne(). If the
// repository in question does not exist, sql.ErrNoRows is returned.
func FindRepositoryByID(db gorp.SqlExecutor, id int64) (*models.Repository, error) {
	var repo models.Repository
	err := db.SelectOne(&repo,
		"SELECT * FROM repos WHERE id = $1", id)
	return &repo, err
}

func GetSecurityInfo(db gorp.SqlExecutor, repoID int64, manifestDigest digest.Digest) (*models.TrivySecurityInfo, error) {
	var securityInfo *models.TrivySecurityInfo
	err := db.SelectOne(&securityInfo,
		"SELECT * FROM trivy_security_info WHERE repo_id = $1 and digest = $2",
		repoID, manifestDigest,
	)

	return securityInfo, err
}
