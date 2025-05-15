// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package models

import (
	"time"

	"github.com/opencontainers/go-digest"
)

// Blob contains a record from the `blobs` table.
//
// In the `blobs` table, blobs are only bound to an account. This makes
// cross-repo blob mounts cheap and easy to implement. The actual connection to
// repos is in the `blob_mounts` table.
//
// StorageID is used to construct the filename (or equivalent) for this blob
// in the StorageDriver. We cannot use the digest for this since the StorageID
// needs to be chosen at the start of the blob upload, when the digest is not
// known yet.
type Blob struct {
	ID                     int64         `db:"id"`
	AccountName            AccountName   `db:"account_name"`
	Digest                 digest.Digest `db:"digest"`
	SizeBytes              uint64        `db:"size_bytes"`
	StorageID              string        `db:"storage_id"`
	MediaType              string        `db:"media_type"`
	PushedAt               time.Time     `db:"pushed_at"`
	NextValidationAt       time.Time     `db:"next_validation_at"` // see tasks.BlobValidationJob
	ValidationErrorMessage string        `db:"validation_error_message"`
	CanBeDeletedAt         *time.Time    `db:"can_be_deleted_at"` // see tasks.BlobSweepJob
	BlocksVulnScanning     *bool         `db:"blocks_vuln_scanning"`
}

// SafeMediaType returns the MediaType field, but falls back to "application/octet-stream" if it is empty.
func (b Blob) SafeMediaType() string {
	if b.MediaType == "" {
		return "application/octet-stream"
	}
	return b.MediaType
}

const (
	// BlobValidationInterval is how often each blob will be validated by BlobValidationJob.
	// This is here instead of near the job because package processor also needs to know it.
	BlobValidationInterval = 7 * 24 * time.Hour
	// BlobValidationAfterErrorInterval is how quickly BlobValidationJob will
	// retry a failed blob validation.
	BlobValidationAfterErrorInterval = 10 * time.Minute
)

// Upload contains a record from the `uploads` table.
//
// Digest contains the SHA256 digest of everything that has been uploaded so
// far. This is used to validate that we're resuming at the right position in
// the next PUT/PATCH.
type Upload struct {
	RepositoryID int64     `db:"repo_id"`
	UUID         string    `db:"uuid"`
	StorageID    string    `db:"storage_id"`
	SizeBytes    uint64    `db:"size_bytes"`
	Digest       string    `db:"digest"`
	NumChunks    uint32    `db:"num_chunks"`
	UpdatedAt    time.Time `db:"updated_at"`
}
