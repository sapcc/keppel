// SPDX-FileCopyrightText: 2018-2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"context"
	"database/sql"
	"errors"

	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/models"
)

var (
	findAccountQuery        = models.AccountStore.MustPrepareSelectQueryWhere(`name = $1`)
	findReducedAccountQuery = models.ReducedAccountStore.MustPrepareSelectQueryWhere(`name = $1`)
)

// FindAccount is a convenience wrapper around Store.SelectOne().
// If the blob in question does not exist, sql.ErrNoRows is returned.
func FindAccount(ctx context.Context, db DBInterface, name models.AccountName) (models.Account, error) {
	return findAccountQuery.SelectOne(ctx, db, name)
}

// FindReducedAccount is like FindAccount, but it returns a ReducedAccount instead.
// This can be significantly faster than FindAccount if only the most common stuff is needed.
func FindReducedAccount(ctx context.Context, db DBInterface, name models.AccountName) (models.ReducedAccount, error) {
	return findReducedAccountQuery.SelectOne(ctx, db, name)
}

// DoesAccountExist checks if an account with the given name exists in the DB.
func DoesAccountExist(db DBInterface, name models.AccountName) (bool, error) {
	return SelectOneValue[bool](db, `SELECT COUNT(*) > 0 FROM accounts WHERE name = $1`, name)
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

var blobGetQueryByAccountName = models.BlobStore.MustPrepareSelectQueryWhere(`account_name = $1 AND digest = $2`)

// FindBlobByRepositoryName is a convenience wrapper around Store.SelectOne().
// If the blob in question does not exist, sql.ErrNoRows is returned.
func FindBlobByRepositoryName(ctx context.Context, db DBInterface, blobDigest digest.Digest, repoName string, accountName models.AccountName) (models.Blob, error) {
	return models.BlobStore.SelectOne(ctx, db, blobGetQueryByRepoName, accountName, blobDigest.String(), repoName)
}

// FindBlobByRepository is a convenience wrapper around Store.SelectOne().
// If the blob in question does not exist, sql.ErrNoRows is returned.
func FindBlobByRepository(ctx context.Context, db DBInterface, blobDigest digest.Digest, repo models.ReducedRepository) (models.Blob, error) {
	return models.BlobStore.SelectOne(ctx, db, blobGetQueryByRepoID, repo.AccountName, blobDigest.String(), repo.ID)
}

// FindBlobByAccountName is a convenience wrapper around Store.SelectOne().
// If the blob in question does not exist, sql.ErrNoRows is returned.
func FindBlobByAccountName(ctx context.Context, db DBInterface, blobDigest digest.Digest, accountName models.AccountName) (models.Blob, error) {
	return blobGetQueryByAccountName.SelectOne(ctx, db, accountName, blobDigest.String())
}

// MountBlobIntoRepo creates an entry in the blob_mounts database table.
func MountBlobIntoRepo(db DBInterface, blob models.Blob, repo models.ReducedRepository) error {
	_, err := db.Exec(
		`INSERT INTO blob_mounts (blob_id, repo_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		blob.ID, repo.ID,
	)
	return err
}

var uploadGetQueryByRepoID = models.UploadStore.MustPrepareSelectQueryWhere(`uuid = $1 AND repo_id = $2`)

// FindUploadByRepository is a convenience wrapper around Store.SelectOne().
// If the upload in question does not exist, sql.ErrNoRows is returned.
func FindUploadByRepository(ctx context.Context, db DBInterface, uuid string, repo models.ReducedRepository) (models.Upload, error) {
	return uploadGetQueryByRepoID.SelectOne(ctx, db, uuid, repo.ID)
}

var (
	manifestGetQueryByRepoID   = models.ManifestStore.MustPrepareSelectQueryWhere(`repo_id = $1 AND digest = $2`)
	manifestGetQueryByRepoName = models.ManifestStore.MustPrepareSelectQueryWhere(
		`repo_id = (SELECT id FROM repos WHERE account_name = $1 AND name = $2) AND digest = $3`)
)

// FindManifest is a convenience wrapper around Store.SelectOne().
// If the manifest in question does not exist, sql.ErrNoRows is returned.
func FindManifest(ctx context.Context, db DBInterface, repo models.ReducedRepository, manifestDigest digest.Digest) (models.Manifest, error) {
	return manifestGetQueryByRepoID.SelectOne(ctx, db, repo.ID, manifestDigest.String())
}

// FindManifestByRepositoryName is a convenience wrapper around Store.SelectOne().
// If the manifest in question does not exist, sql.ErrNoRows is returned.
func FindManifestByRepositoryName(ctx context.Context, db DBInterface, repoName string, accountName models.AccountName, manifestDigest digest.Digest) (models.Manifest, error) {
	return manifestGetQueryByRepoName.SelectOne(ctx, db, accountName, repoName, manifestDigest.String())
}

var (
	quotasGetQueryByAuthTenantID = models.QuotasStore.MustPrepareSelectQueryWhere(`auth_tenant_id = $1`)
)

// FindQuotas works similar to Store.SelectOne().
// If the quota does not exist, sql.ErrNoRows is returned.
func FindQuotas(ctx context.Context, db DBInterface, authTenantID string) (models.Quotas, error) {
	return quotasGetQueryByAuthTenantID.SelectOne(ctx, db, authTenantID)
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
func GetManifestUsage(db DBInterface, quotas models.Quotas) (uint64, error) {
	return SelectOneValue[uint64](db, manifestUsageQuery, quotas.AuthTenantID)
}

// AtLeastZero safely converts int or int64 values (which might come from
// DB.SelectInt() or from IO reads/writes) to uint64 by clamping negative values to 0.
func AtLeastZero[I interface{ int | int64 }](x I) uint64 {
	if x < 0 {
		return 0
	}
	return uint64(x)
}

// FindOrCreateRepository works similar to Store.SelectOne(), but autovivifies a
// Repository record when none exists yet.
func FindOrCreateRepository(ctx context.Context, db DBInterface, name string, accountName models.AccountName) (models.Repository, error) {
	repo, err := FindRepository(ctx, db, name, accountName)
	if errors.Is(err, sql.ErrNoRows) {
		repo = models.Repository{
			Name:        name,
			AccountName: accountName,
		}
		err = models.RepositoryStore.Insert(ctx, db, &repo)
	}
	return repo, err
}

var (
	repoGetQueryByName        = models.RepositoryStore.MustPrepareSelectQueryWhere(`account_name = $1 AND name = $2`)
	repoGetQueryByID          = models.RepositoryStore.MustPrepareSelectQueryWhere(`id = $1`)
	reducedRepoGetQueryByName = models.ReducedRepositoryStore.MustPrepareSelectQueryWhere(`account_name = $1 AND name = $2`)
)

// FindRepository is a convenience wrapper around Store.SelectOne().
// If the repository in question does not exist, sql.ErrNoRows is returned.
func FindRepository(ctx context.Context, db DBInterface, name string, accountName models.AccountName) (models.Repository, error) {
	return repoGetQueryByName.SelectOne(ctx, db, accountName, name)
}

// FindOrCreateReducedRepository is like [FindOrCreateRepository], but is faster because it returns a ReducedRepository.
func FindOrCreateReducedRepository(ctx context.Context, db DBInterface, name string, accountName models.AccountName) (models.ReducedRepository, error) {
	repo, err := FindReducedRepository(ctx, db, name, accountName)
	if errors.Is(err, sql.ErrNoRows) {
		fullRepo := models.Repository{
			Name:        name,
			AccountName: accountName,
		}
		err = models.RepositoryStore.Insert(ctx, db, &fullRepo)
		repo = fullRepo.Reduced()
	}
	return repo, err
}

// FindReducedRepository is like [FindRepository], but is faster because it returns a ReducedRepository.
func FindReducedRepository(ctx context.Context, db DBInterface, name string, accountName models.AccountName) (models.ReducedRepository, error) {
	return reducedRepoGetQueryByName.SelectOne(ctx, db, accountName, name)
}

// FindRepositoryByID is a convenience wrapper around Store.SelectOne().
// If the repository in question does not exist, sql.ErrNoRows is returned.
func FindRepositoryByID(ctx context.Context, db DBInterface, id int64) (models.Repository, error) {
	return repoGetQueryByID.SelectOne(ctx, db, id)
}

var securityInfoGetQueryByRepoID = models.TrivySecurityInfoStore.MustPrepareSelectQueryWhere(`repo_id = $1 AND digest = $2`)

// GetSecurityInfo is a convenience wrapper around Store.SelectOne().
// If the securityInfo in question does not exist, sql.ErrNoRows is returned.
func GetSecurityInfo(ctx context.Context, db DBInterface, repoID int64, manifestDigest digest.Digest) (models.TrivySecurityInfo, error) {
	return securityInfoGetQueryByRepoID.SelectOne(ctx, db, repoID, manifestDigest.String())
}

var peerGetQueryByHostname = models.PeerStore.MustPrepareSelectQueryWhere(`hostname = $1`)

// FindPeer is a convenience wrapper around Store.SelectOne().
// If the peer in question does not exist, sql.ErrNoRows is returned.
func FindPeer(ctx context.Context, db DBInterface, hostname string) (models.Peer, error) {
	return peerGetQueryByHostname.SelectOne(ctx, db, hostname)
}
