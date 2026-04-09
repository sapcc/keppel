// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package models

// AccountName identifies an account.
// This typedef is used to distinguish these names from other string values.
type AccountName string

// RepositoryName identifies a repository.
// This typedef is used to distinguish these names from other string values.
type RepositoryName string

// BlobID is an ID into the `blobs` table.
// This typedef is used to distinguish these IDs from IDs of other tables or raw int64 values.
type BlobID int64

// RepositoryID is an ID into the `repos` table.
// This typedef is used to distinguish these IDs from IDs of other tables or raw int64 values.
type RepositoryID int64
