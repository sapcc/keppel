// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package models

import (
	"time"

	. "github.com/majewsky/gg/option"
)

// Repository contains a record from the `repos` table.
type Repository struct {
	ID                      RepositoryID      `db:"id"`
	AccountName             AccountName       `db:"account_name"`
	Name                    RepositoryName    `db:"name"`
	NextBlobMountSweepAt    Option[time.Time] `db:"next_blob_mount_sweep_at"` // see tasks.BlobMountSweepJob
	NextManifestSyncAt      Option[time.Time] `db:"next_manifest_sync_at"`    // see tasks.ManifestSyncJob (only set for replica accounts)
	NextGarbageCollectionAt Option[time.Time] `db:"next_gc_at"`               // see tasks.GarbageCollectManifestsJob
}

// FullName prepends the account name to the repository name.
func (r Repository) FullName() RepositoryName {
	return RepositoryName(string(r.AccountName) + `/` + string(r.Name))
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
	ID          RepositoryID
	AccountName AccountName
	Name        RepositoryName

	// NOTE: When adding or removing fields, always adjust Repository.Reduced() and keppel.FindReducedRepository() too!
}

// FullName prepends the account name to the repository name.
func (r ReducedRepository) FullName() RepositoryName {
	return RepositoryName(string(r.AccountName) + `/` + string(r.Name))
}
