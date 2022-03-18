/******************************************************************************
*
*  Copyright 2020 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package tasks

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/easypg"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestSweepBlobs(t *testing.T) {
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)

	//insert some blobs into the DB
	var dbBlobs []keppel.Blob
	for idx := int64(0); idx < 5; idx++ {
		blob := test.GenerateExampleLayer(idx)
		dbBlobs = append(dbBlobs, blob.MustUpload(t, s, fooRepoRef))
	}

	//since uploadBlob() mounts these blobs into the test1/foo repository, there
	//should be nothing to clean up; SweepBlobsInNextAccount() should only set
	//the blobs_sweeped_at timestamp on the account
	expectSuccess(t, j.SweepBlobsInNextAccount())
	expectError(t, sql.ErrNoRows.Error(), j.SweepBlobsInNextAccount())
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/blob-sweep-001.sql")
	s.ExpectBlobsExistInStorage(t, dbBlobs...)

	//remove blob mounts for some blobs - SweepBlobsInNextAccount() should now
	//mark them for deletion (but not actually delete them yet)
	s.Clock.StepBy(2 * time.Hour)
	mustExec(t, s.DB,
		`DELETE FROM blob_mounts WHERE blob_id IN ($1,$2,$3)`,
		dbBlobs[0].ID, dbBlobs[1].ID, dbBlobs[2].ID,
	)
	expectSuccess(t, j.SweepBlobsInNextAccount())
	expectError(t, sql.ErrNoRows.Error(), j.SweepBlobsInNextAccount())
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/blob-sweep-002.sql")
	s.ExpectBlobsExistInStorage(t, dbBlobs...)

	//recreate one of these blob mounts - this should protect it from being
	//deleted
	mustExec(t, s.DB,
		`INSERT INTO blob_mounts (blob_id, repo_id) VALUES ($1,1)`,
		dbBlobs[2].ID,
	)

	//the other two blobs should get deleted in the next sweep
	s.Clock.StepBy(2 * time.Hour)
	expectSuccess(t, j.SweepBlobsInNextAccount())
	expectError(t, sql.ErrNoRows.Error(), j.SweepBlobsInNextAccount())
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/blob-sweep-003.sql")
	s.ExpectBlobsMissingInStorage(t, dbBlobs[0:2]...)
	s.ExpectBlobsExistInStorage(t, dbBlobs[2:]...)
}

func TestValidateBlobs(t *testing.T) {
	j, s := setup(t)
	s.Clock.StepBy(1 * time.Hour)

	//upload some blobs (we need to step the clock after each upload to ensure
	//that ValidateNextBlob later goes through them in a particular order, to
	//make the testcase deterministic)
	dbBlobs := make([]keppel.Blob, 3)
	for idx := range dbBlobs {
		blob := test.GenerateExampleLayer(int64(idx))
		s.Clock.Step()
		dbBlobs[idx] = blob.MustUpload(t, s, fooRepoRef)
	}

	//ValidateNextBlob should be happy about these blobs
	s.Clock.StepBy(8*24*time.Hour - 2*time.Second)
	expectSuccess(t, j.ValidateNextBlob())
	s.Clock.Step()
	expectSuccess(t, j.ValidateNextBlob())
	s.Clock.Step()
	expectSuccess(t, j.ValidateNextBlob())
	expectError(t, sql.ErrNoRows.Error(), j.ValidateNextBlob())
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/blob-validate-001.sql")

	//deliberately destroy one of the blob's digests
	wrongDigest := digest.Canonical.FromBytes([]byte("not the right content"))
	mustExec(t, s.DB,
		`UPDATE blobs SET digest = $1 WHERE digest = $2`,
		wrongDigest.String(), dbBlobs[2].Digest,
	)

	//not so happy now, huh?
	s.Clock.StepBy(8*24*time.Hour - 2*time.Second)
	expectedError := fmt.Sprintf(
		`while validating a blob: expected digest %s, but got %s`,
		wrongDigest.String(), dbBlobs[2].Digest,
	)
	expectSuccess(t, j.ValidateNextBlob())
	s.Clock.Step()
	expectSuccess(t, j.ValidateNextBlob())
	s.Clock.Step()
	expectError(t, expectedError, j.ValidateNextBlob())
	expectError(t, sql.ErrNoRows.Error(), j.ValidateNextBlob())
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/blob-validate-002.sql")

	//fix the issue
	mustExec(t, s.DB,
		`UPDATE blobs SET digest = $1 WHERE digest = $2`,
		dbBlobs[2].Digest, wrongDigest.String(),
	)

	//this should resolve the error and also remove the error message from the DB
	//(note that the order in which blobs are checked differs this time because
	//blobs with an existing validation error are chosen with higher priority)
	s.Clock.StepBy(8*24*time.Hour - 2*time.Second)
	expectSuccess(t, j.ValidateNextBlob())
	s.Clock.Step()
	expectSuccess(t, j.ValidateNextBlob())
	s.Clock.Step()
	expectSuccess(t, j.ValidateNextBlob())
	expectError(t, sql.ErrNoRows.Error(), j.ValidateNextBlob())
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/blob-validate-003.sql")
}
