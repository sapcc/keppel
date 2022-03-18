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
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/sapcc/go-bits/easypg"
	uuid "github.com/satori/go.uuid"

	"github.com/sapcc/keppel/internal/keppel"
)

var (
	testUploadUUID = uuid.NewV4().String()
	testStorageID  = keppel.GenerateStorageID()
)

func TestDeleteAbandonedUploadWithZeroChunks(t *testing.T) {
	testDeleteUpload(t, func(sd keppel.StorageDriver, account keppel.Account) keppel.Upload {
		return keppel.Upload{
			SizeBytes: 0,
			Digest:    "",
			NumChunks: 0,
		}
	})
}

func TestDeleteAbandonedUploadWithOneChunk(t *testing.T) {
	testDeleteUpload(t, func(sd keppel.StorageDriver, account keppel.Account) keppel.Upload {
		data := "just some test data"
		err := sd.AppendToBlob(account, testStorageID, 1, p2len(data), strings.NewReader(data))
		if err != nil {
			t.Fatal(err.Error())
		}

		return keppel.Upload{
			SizeBytes: uint64(len(data)),
			Digest:    sha256Of([]byte(data)),
			NumChunks: 1,
		}
	})
}

func TestDeleteAbandonedUploadWithManyChunks(t *testing.T) {
	testDeleteUpload(t, func(sd keppel.StorageDriver, account keppel.Account) keppel.Upload {
		chunks := []string{"just", "some", "test", "data"}
		for idx, data := range chunks {
			err := sd.AppendToBlob(account, testStorageID, uint32(idx+1), p2len(data), strings.NewReader(data))
			if err != nil {
				t.Fatalf("AppendToBlob %d failed: %s", idx, err.Error())
			}
		}

		fullData := strings.Join(chunks, "")
		return keppel.Upload{
			SizeBytes: uint64(len(fullData)),
			Digest:    sha256Of([]byte(fullData)),
			NumChunks: 1,
		}
	})
}

func testDeleteUpload(t *testing.T, setupUploadObject func(keppel.StorageDriver, keppel.Account) keppel.Upload) {
	j, s := setup(t)
	account := keppel.Account{Name: "test1"}

	//right now, there are no upload objects, so DeleteNextAbandonedUpload should indicate that
	s.Clock.StepBy(48 * time.Hour)
	expectNoRows(t, j.DeleteNextAbandonedUpload())

	//create the upload object for this test
	upload := setupUploadObject(s.SD, account)
	//apply common attributes
	upload.RepositoryID = 1
	upload.UUID = testUploadUUID
	upload.StorageID = testStorageID
	upload.UpdatedAt = s.Clock.Now()
	err := s.DB.Insert(&upload)
	if err != nil {
		t.Fatal(err.Error())
	}

	//DeleteNextAbandonedUpload should not do anything since this upload is fairly recent
	s.Clock.StepBy(3 * time.Hour)
	expectNoRows(t, j.DeleteNextAbandonedUpload())

	//after a day has passed, DeleteNextAbandonedUpload should clean up this upload
	s.Clock.StepBy(24 * time.Hour)
	err = j.DeleteNextAbandonedUpload()
	if err != nil {
		t.Errorf("expected no error, but got: %s", err.Error())
	}

	//now the DB should not contain any traces of the upload, only the account and repo
	easypg.AssertDBContent(t, s.DB.DbMap.Db, "fixtures/after-delete-upload.sql")

	//and once again, DeleteNextAbandonedUpload should indicate that there's nothing to do
	expectNoRows(t, j.DeleteNextAbandonedUpload())
}

func expectNoRows(t *testing.T, err error) {
	t.Helper()
	switch err {
	case sql.ErrNoRows:
		return
	case nil:
		t.Error("expected sql.ErrNoRows, but got no error")
	default:
		t.Errorf("expected sql.ErrNoRows, but got: %s", err.Error())
	}
}

func sha256Of(data []byte) string {
	sha256Hash := sha256.Sum256(data)
	return hex.EncodeToString(sha256Hash[:])
}

func p2len(data string) *uint64 {
	x := uint64(len(data))
	return &x
}
