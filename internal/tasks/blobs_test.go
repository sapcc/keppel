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
	"testing"
	"time"

	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestSweepBlobs(t *testing.T) {
	j, _, db, sd, clock, _ := setup(t)
	clock.StepBy(1 * time.Hour)

	//insert some blobs into the DB
	var dbBlobs []keppel.Blob
	for idx := int64(0); idx < 5; idx++ {
		blob := test.GenerateExampleLayer(idx)
		dbBlob := uploadBlob(t, db, sd, clock, blob)
		dbBlobs = append(dbBlobs, dbBlob)
	}

	//since uploadBlob() mounts these blobs into the test1/foo repository, there
	//should be nothing to clean up; SweepBlobsInNextAccount() should only set
	//the blobs_sweeped_at timestamp on the account
	expectSuccess(t, j.SweepBlobsInNextAccount())
	expectError(t, sql.ErrNoRows.Error(), j.SweepBlobsInNextAccount())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/blob-sweep-001.sql")
	expectBlobsExistInStorage(t, sd, dbBlobs...)

	//remove blob mounts for some blobs - SweepBlobsInNextAccount() should now
	//mark them for deletion (but not actually delete them yet)
	clock.StepBy(2 * time.Hour)
	mustExec(t, db,
		`DELETE FROM blob_mounts WHERE blob_id IN ($1,$2,$3)`,
		dbBlobs[0].ID, dbBlobs[1].ID, dbBlobs[2].ID,
	)
	expectSuccess(t, j.SweepBlobsInNextAccount())
	expectError(t, sql.ErrNoRows.Error(), j.SweepBlobsInNextAccount())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/blob-sweep-002.sql")
	expectBlobsExistInStorage(t, sd, dbBlobs...)

	//recreate one of these blob mounts - this should protect it from being
	//deleted
	mustExec(t, db,
		`INSERT INTO blob_mounts (blob_id, repo_id) VALUES ($1,1)`,
		dbBlobs[2].ID,
	)

	//the other two blobs should get deleted in the next sweep
	clock.StepBy(2 * time.Hour)
	expectSuccess(t, j.SweepBlobsInNextAccount())
	expectError(t, sql.ErrNoRows.Error(), j.SweepBlobsInNextAccount())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/blob-sweep-003.sql")
	expectBlobsMissingInStorage(t, sd, dbBlobs[0:2]...)
	expectBlobsExistInStorage(t, sd, dbBlobs[2:]...)
}
