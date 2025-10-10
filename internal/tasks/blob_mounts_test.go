// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"database/sql"
	"testing"
	"time"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"

	"github.com/sapcc/keppel/internal/test"
)

func TestSweepBlobMounts(t *testing.T) {
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)

	sweepBlobMountsJob := j.BlobMountSweepJob(s.Registry)

	// setup an image manifest with some layers, so that we have some blob mounts
	// that shall not be sweeped
	image := test.GenerateImage(
		test.GenerateExampleLayer(1),
		test.GenerateExampleLayer(2),
	)
	image.MustUpload(t, s, fooRepoRef, "")

	// the blob mount sweep should not mark any blob mount for deletion since they
	// are all in use, but should set the blob_mounts_sweeped_at timestamp on the
	// repo
	assert.ErrEqual(t, sweepBlobMountsJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, sweepBlobMountsJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/blob-mount-sweep-001.sql")

	// upload two blobs that are not referenced by any manifest
	s.Clock.StepBy(2 * time.Hour)
	bogusBlob1 := test.GenerateExampleLayer(3)
	bogusBlob2 := test.GenerateExampleLayer(4)
	dbBogusBlob1 := bogusBlob1.MustUpload(t, s, fooRepoRef)
	dbBogusBlob2 := bogusBlob2.MustUpload(t, s, fooRepoRef)
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/blob-mount-sweep-002.sql")

	// the next sweep should mark those blob's mounts for deletion
	assert.ErrEqual(t, sweepBlobMountsJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, sweepBlobMountsJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/blob-mount-sweep-003.sql")

	// save one of those blob mounts from deletion by creating a manifest-blob
	// reference for it (this reference is actually bogus and would be removed by
	// the ManifestValidationJob, but we're not testing that here)
	test.MustExec(t, s.DB,
		`INSERT INTO manifest_blob_refs (blob_id, repo_id, digest) VALUES ($1, 1, $2)`,
		dbBogusBlob2.ID, image.Manifest.Digest.String(),
	)
	_ = dbBogusBlob1

	// the next sweep will delete the mount for `bogusBlob1` (since it was marked
	// for deletion and is still not referenced by any manifest), but remove the
	// mark on the mount for `bogusBlob2` (since it is now referenced by a
	// manifest)
	s.Clock.StepBy(2 * time.Hour)
	assert.ErrEqual(t, sweepBlobMountsJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, sweepBlobMountsJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/blob-mount-sweep-004.sql")
}
