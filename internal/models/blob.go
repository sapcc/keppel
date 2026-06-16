// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package models

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/manifest"
	. "go.xyrillian.de/gg/option"
	"go.xyrillian.de/oblast"
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
	ID                     int64             `db:"id,auto"`
	AccountName            AccountName       `db:"account_name"`
	Digest                 digest.Digest     `db:"digest"`
	SizeBytes              uint64            `db:"size_bytes"`
	StorageID              string            `db:"storage_id"`
	MediaType              string            `db:"media_type"`
	PushedAt               time.Time         `db:"pushed_at"`
	NextValidationAt       time.Time         `db:"next_validation_at"` // see tasks.BlobValidationJob
	ValidationErrorMessage string            `db:"validation_error_message"`
	CanBeDeletedAt         Option[time.Time] `db:"can_be_deleted_at"` // see tasks.BlobSweepJob
	BlocksVulnScanning     Option[bool]      `db:"blocks_vuln_scanning"`
}

// BlobStore provides loading and storing of [Blob] objects from the DB.
var BlobStore = oblast.MustNewStore[Blob](
	oblast.PostgresDialect(),
	oblast.TableNameIs("blobs"),
	oblast.PrimaryKeyIs("id"),
)

// SafeMediaType returns the MediaType field, but falls back to "application/octet-stream" if it is empty.
func (b Blob) SafeMediaType() string {
	if b.MediaType == "" {
		return "application/octet-stream"
	}
	return b.MediaType
}

// BlobCompression is an enum indicating the compression format used within the blob.
// This information is derived from the blob's MediaType, and only reported when the MediaType is understood by Keppel.
type BlobCompression string

const (
	// BlobCompressionUnknown indicates that we do not recognize the blob's MediaType, and thus do not know whether it is uncompressed.
	BlobCompressionUnknown BlobCompression = "unknown"
	// BlobCompressionNone indicates that we recognize the blob's MediaType and thus know that the payload is uncompressed.
	BlobCompressionNone BlobCompression = "none"
	BlobCompressionGzip BlobCompression = "gzip"
	BlobCompressionZstd BlobCompression = "zstd"
)

// Compression reports the compression format used within the blob,
// or BlobCompressionUnknown if the blob's MediaType is not understood by Keppel.
func (b Blob) Compression() BlobCompression {
	switch b.MediaType {
	case manifest.DockerV2SchemaLayerMediaTypeUncompressed, v1.MediaTypeImageLayer:
		return BlobCompressionNone
	case manifest.DockerV2Schema2LayerMediaType, v1.MediaTypeImageLayerGzip:
		return BlobCompressionGzip
	case manifest.DockerV2SchemaLayerMediaTypeZstd, v1.MediaTypeImageLayerZstd:
		return BlobCompressionZstd
	default:
		return BlobCompressionUnknown
	}
}

// Reader wraps a reader for compressed data in this format with the respective
// decompressor, returning a reader that yields uncompressed data.
func (c BlobCompression) Reader(r io.Reader) (io.ReadCloser, error) {
	switch c {
	case BlobCompressionNone:
		return io.NopCloser(r), nil
	case BlobCompressionGzip:
		return gzip.NewReader(r)
	case BlobCompressionZstd:
		zr, err := zstd.NewReader(r)
		if err != nil {
			return nil, err
		}
		return zr.IOReadCloser(), nil
	case BlobCompressionUnknown:
		return nil, errors.New("do not know how to handle read data with unknown compression format")
	default:
		panic(fmt.Sprintf("unexpected BlobCompression value: %q", c))
	}
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

// UploadStore provides loading and storing of [Upload] objects from the DB.
var UploadStore = oblast.MustNewStore[Upload](
	oblast.PostgresDialect(),
	oblast.TableNameIs("uploads"),
	oblast.PrimaryKeyIs("repo_id", "uuid"),
)
