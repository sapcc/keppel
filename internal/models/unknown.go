// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package models

import (
	"time"

	"github.com/opencontainers/go-digest"
	"go.xyrillian.de/oblast"
)

// UnknownBlob contains a record from the `unknown_blobs` table.
// This is only used by tasks.StorageSweepJob().
type UnknownBlob struct {
	AccountName    AccountName `db:"account_name"`
	StorageID      string      `db:"storage_id"`
	CanBeDeletedAt time.Time   `db:"can_be_deleted_at"`
}

// UnknownBlobStore provides loading and storing of [UnknownBlob] objects from the DB.
var UnknownBlobStore = oblast.MustNewStore[UnknownBlob](
	oblast.PostgresDialect(),
	oblast.TableNameIs("unknown_blobs"),
	oblast.PrimaryKeyIs("account_name", "storage_id"),
)

// UnknownManifest contains a record from the `unknown_manifests` table.
// This is only used by tasks.StorageSweepJob().
//
// NOTE: We don't use repository IDs here because unknown manifests may exist in
// repositories that are also not known to the database.
type UnknownManifest struct {
	AccountName    AccountName   `db:"account_name"`
	RepositoryName string        `db:"repo_name"`
	Digest         digest.Digest `db:"digest"`
	CanBeDeletedAt time.Time     `db:"can_be_deleted_at"`
}

// UnknownManifestStore provides loading and storing of [UnknownManifest] objects from the DB.
var UnknownManifestStore = oblast.MustNewStore[UnknownManifest](
	oblast.PostgresDialect(),
	oblast.TableNameIs("unknown_manifests"),
	oblast.PrimaryKeyIs("account_name", "repo_name", "digest"),
)

// UnknownTrivyReport contains a record from the `unknown_trivy_reports` table.
// This is only used by tasks.StorageSweepJob().
//
// NOTE: We don't use repository IDs here because unknown Trivy reports may exist in
// repositories that are also not known to the database.
type UnknownTrivyReport struct {
	AccountName    AccountName   `db:"account_name"`
	RepositoryName string        `db:"repo_name"`
	Digest         digest.Digest `db:"digest"`
	Format         string        `db:"format"`
	CanBeDeletedAt time.Time     `db:"can_be_deleted_at"`
}

// UnknownTrivyReportStore provides loading and storing of [UnknownTrivyReport] objects from the DB.
var UnknownTrivyReportStore = oblast.MustNewStore[UnknownTrivyReport](
	oblast.PostgresDialect(),
	oblast.TableNameIs("unknown_trivy_reports"),
	oblast.PrimaryKeyIs("account_name", "repo_name", "digest", "format"),
)
