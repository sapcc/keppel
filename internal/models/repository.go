// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package models

import (
	"time"

	. "go.xyrillian.de/gg/option"
	"go.xyrillian.de/oblast"
)

// Repository contains a record from the `repos` table.
type Repository struct {
	ID                      int64             `db:"id,auto"`
	AccountName             AccountName       `db:"account_name"`
	Name                    string            `db:"name"`
	NextBlobMountSweepAt    Option[time.Time] `db:"next_blob_mount_sweep_at"` // see tasks.BlobMountSweepJob
	NextManifestSyncAt      Option[time.Time] `db:"next_manifest_sync_at"`    // see tasks.ManifestSyncJob (only set for replica accounts)
	NextGarbageCollectionAt Option[time.Time] `db:"next_gc_at"`               // see tasks.GarbageCollectManifestsJob
}

// RepositoryStore provides loading and storing of [Repository] objects from the DB.
var RepositoryStore = oblast.MustNewStore[Repository](
	oblast.PostgresDialect(),
	oblast.TableNameIs("repos"),
	oblast.PrimaryKeyIs("id"),
)

// FullName prepends the account name to the repository name.
func (r Repository) FullName() string {
	return string(r.AccountName) + `/` + r.Name
}

// Reduced converts a Repository into a ReducedRepository.
func (r Repository) Reduced() ReducedRepository {
	return ReducedRepository{
		ID:          r.ID,
		AccountName: r.AccountName,
		Name:        r.Name,
	}
}

// ReducedRepository contains just the fields from type Repository that the Registry API is most interested in.
// This type exists to avoid loading non-essential DB fields when we don't need to,
// which is a memory optimization for the keppel-api process.
type ReducedRepository struct {
	ID          int64       `db:"id"`
	AccountName AccountName `db:"account_name"`
	Name        string      `db:"name"`

	// NOTE: When adding or removing fields, always adjust Repository.Reduced() and keppel.FindReducedRepository() too!
}

// ReducedRepositoryStore provides loading and storing of [ReducedRepository] objects from the DB.
var ReducedRepositoryStore = oblast.MustNewStore[ReducedRepository](
	oblast.PostgresDialect(),
	oblast.TableNameIs("repos"),
	oblast.PrimaryKeyIs("id"),
)

// FullName prepends the account name to the repository name.
func (r ReducedRepository) FullName() string {
	return string(r.AccountName) + `/` + r.Name
}
