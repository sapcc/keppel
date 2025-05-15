// SPDX-FileCopyrightText: 2024 SAP SE
// SPDX-License-Identifier: Apache-2.0

package models

import (
	"time"

	"github.com/opencontainers/go-digest"
)

// UnknownBlob contains a record from the `unknown_blobs` table.
// This is only used by tasks.StorageSweepJob().
type UnknownBlob struct {
	AccountName    AccountName `db:"account_name"`
	StorageID      string      `db:"storage_id"`
	CanBeDeletedAt time.Time   `db:"can_be_deleted_at"`
}

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
