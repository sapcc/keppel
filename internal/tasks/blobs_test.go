// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"

	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestSweepBlobs(t *testing.T) {
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)

	sweepBlobsJob := j.BlobSweepJob(s.Registry)

	// insert some blobs into the DB
	var dbBlobs []models.Blob
	for idx := range 5 {
		blob := test.GenerateExampleLayer(int64(idx))
		dbBlobs = append(dbBlobs, blob.MustUpload(t, s, fooRepoRef))
	}

	// since uploadBlob() mounts these blobs into the test1/foo repository, there
	// should be nothing to clean up; BlobSweepJob should only set
	// the blobs_sweeped_at timestamp on the account
	assert.ErrEqual(t, sweepBlobsJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, sweepBlobsJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/blob-sweep-001.sql")
	s.ExpectBlobsExistInStorage(t, dbBlobs...)

	// remove blob mounts for some blobs - BlobSweepJob should now
	// mark them for deletion (but not actually delete them yet)
	s.Clock.StepBy(2 * time.Hour)
	test.MustExec(t, s.DB, `DELETE FROM blob_mounts WHERE blob_id IN ($1,$2,$3)`,
		dbBlobs[0].ID, dbBlobs[1].ID, dbBlobs[2].ID,
	)
	assert.ErrEqual(t, sweepBlobsJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, sweepBlobsJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/blob-sweep-002.sql")
	s.ExpectBlobsExistInStorage(t, dbBlobs...)

	// recreate one of these blob mounts - this should protect it from being
	// deleted
	test.MustExec(t, s.DB, `INSERT INTO blob_mounts (blob_id, repo_id) VALUES ($1,1)`, dbBlobs[2].ID)

	// the other two blobs should get deleted in the next sweep
	s.Clock.StepBy(2 * time.Hour)
	assert.ErrEqual(t, sweepBlobsJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, sweepBlobsJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/blob-sweep-003.sql")
	s.ExpectBlobsMissingInStorage(t, dbBlobs[0:2]...)
	s.ExpectBlobsExistInStorage(t, dbBlobs[2:]...)
}

func TestValidateBlobs(t *testing.T) {
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)
	validateBlobJob := j.BlobValidationJob(s.Registry)

	// upload some blobs (we need to step the clock after each upload to ensure
	// that BlobValidationJob later goes through them in a particular order, to
	// make the testcase deterministic)
	dbBlobs := make([]models.Blob, 3)
	for idx := range dbBlobs {
		blob := test.GenerateExampleLayer(int64(idx))
		s.Clock.StepBy(time.Second)
		dbBlobs[idx] = blob.MustUpload(t, s, fooRepoRef)
	}

	// BlobValidationJob should be happy about these blobs
	s.Clock.StepBy(8*24*time.Hour - 2*time.Second)
	assert.ErrEqual(t, validateBlobJob.ProcessOne(s.Ctx), nil)
	s.Clock.StepBy(time.Second)
	assert.ErrEqual(t, validateBlobJob.ProcessOne(s.Ctx), nil)
	s.Clock.StepBy(time.Second)
	assert.ErrEqual(t, validateBlobJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, validateBlobJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/blob-validate-001.sql")

	// deliberately destroy one of the blob's digests
	wrongDigest := digest.Canonical.FromBytes([]byte("not the right content"))
	test.MustExec(t, s.DB,
		`UPDATE blobs SET digest = $1 WHERE digest = $2`,
		wrongDigest.String(), dbBlobs[2].Digest,
	)

	// not so happy now, huh?
	s.Clock.StepBy(8*24*time.Hour - 2*time.Second)
	expectedError := fmt.Sprintf(
		"expected digest %s, but got %s",
		wrongDigest.String(), dbBlobs[2].Digest,
	)
	assert.ErrEqual(t, validateBlobJob.ProcessOne(s.Ctx), nil)
	s.Clock.StepBy(time.Second)
	assert.ErrEqual(t, validateBlobJob.ProcessOne(s.Ctx), nil)
	s.Clock.StepBy(time.Second)
	assert.ErrEqual(t, validateBlobJob.ProcessOne(s.Ctx), expectedError)
	assert.ErrEqual(t, validateBlobJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/blob-validate-002.sql")

	// fix the issue
	test.MustExec(t, s.DB, `UPDATE blobs SET digest = $1 WHERE digest = $2`,
		dbBlobs[2].Digest, wrongDigest.String(),
	)

	// this should resolve the error and also remove the error message from the DB
	// (note that the order in which blobs are checked differs this time because
	// blobs with an existing validation error are chosen with higher priority)
	s.Clock.StepBy(8*24*time.Hour - 2*time.Second)
	assert.ErrEqual(t, validateBlobJob.ProcessOne(s.Ctx), nil)
	s.Clock.StepBy(time.Second)
	assert.ErrEqual(t, validateBlobJob.ProcessOne(s.Ctx), nil)
	s.Clock.StepBy(time.Second)
	assert.ErrEqual(t, validateBlobJob.ProcessOne(s.Ctx), nil)
	assert.ErrEqual(t, validateBlobJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/blob-validate-003.sql")
}
