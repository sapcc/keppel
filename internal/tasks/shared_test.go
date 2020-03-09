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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
	"gopkg.in/gorp.v2"
)

func setup(t *testing.T) (*Janitor, keppel.Configuration, *keppel.DB, keppel.StorageDriver, *test.Clock) {
	cfg, db := test.Setup(t)

	ad, err := keppel.NewAuthDriver("unittest")
	must(t, err)
	sd, err := keppel.NewStorageDriver("in-memory-for-testing", ad, cfg)
	must(t, err)

	must(t, db.Insert(&keppel.Account{Name: "test1", AuthTenantID: "test1authtenant"}))
	must(t, db.Insert(&keppel.Repository{AccountName: "test1", Name: "foo"}))

	clock := &test.Clock{}
	j := NewJanitor(sd, db).OverrideTimeNow(clock.Now)

	return j, cfg, db, sd, clock
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err.Error())
	}
}

func mustExec(t *testing.T, db gorp.SqlExecutor, query string, args ...interface{}) {
	t.Helper()
	_, err := db.Exec(query, args...)
	if err != nil {
		t.Fatal(err.Error())
	}
}

func expectSuccess(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Error("expected err = nil, but got: " + err.Error())
	}
}

func expectError(t *testing.T, expected string, actual error) {
	t.Helper()
	if actual == nil {
		t.Errorf("expected err = %q, but got <nil>", expected)
	} else if expected != actual.Error() {
		t.Errorf("expected err = %q, but got %q", expected, actual.Error())
	}
}

func uploadBlob(t *testing.T, db *keppel.DB, sd keppel.StorageDriver, clock *test.Clock, blob test.Bytes) int64 {
	t.Helper()
	account := keppel.Account{Name: "test1"}
	repo := keppel.Repository{ID: 1, Name: "foo", AccountName: "test1"}

	//get a storage ID deterministically
	hash := sha256.Sum256(blob.Contents)
	storageID := hex.EncodeToString(hash[:])

	dbBlob := keppel.Blob{
		AccountName: "test1",
		Digest:      blob.Digest.String(),
		SizeBytes:   uint64(len(blob.Contents)),
		StorageID:   storageID,
		PushedAt:    clock.Now(),
		ValidatedAt: clock.Now(),
	}
	must(t, db.Insert(&dbBlob))
	must(t, sd.AppendToBlob(account, storageID, 1, &dbBlob.SizeBytes, bytes.NewBuffer(blob.Contents)))
	must(t, sd.FinalizeBlob(account, storageID, 1))
	must(t, keppel.MountBlobIntoRepo(db, dbBlob, repo))
	return dbBlob.ID
}

func uploadManifest(t *testing.T, db *keppel.DB, sd keppel.StorageDriver, clock *test.Clock, manifest test.Bytes, sizeBytes int) {
	t.Helper()
	account := keppel.Account{Name: "test1"}

	must(t, db.Insert(&keppel.Manifest{
		RepositoryID: 1,
		Digest:       manifest.Digest.String(),
		MediaType:    manifest.MediaType,
		SizeBytes:    uint64(sizeBytes),
		PushedAt:     clock.Now(),
		ValidatedAt:  clock.Now(),
	}))
	must(t, sd.WriteManifest(account, "foo", manifest.Digest.String(), manifest.Contents))
}
