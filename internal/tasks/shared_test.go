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
	"io/ioutil"
	"testing"

	"github.com/opencontainers/go-digest"
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
	sidGen := &test.StorageIDGenerator{}
	j := NewJanitor(cfg, sd, db).OverrideTimeNow(clock.Now).OverrideGenerateStorageID(sidGen.Next)

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

func uploadBlob(t *testing.T, db *keppel.DB, sd keppel.StorageDriver, clock *test.Clock, blob test.Bytes) keppel.Blob {
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
	return dbBlob
}

func uploadManifest(t *testing.T, db *keppel.DB, sd keppel.StorageDriver, clock *test.Clock, manifest test.Bytes, sizeBytes uint64) {
	t.Helper()
	account := keppel.Account{Name: "test1"}

	must(t, db.Insert(&keppel.Manifest{
		RepositoryID: 1,
		Digest:       manifest.Digest.String(),
		MediaType:    manifest.MediaType,
		SizeBytes:    sizeBytes,
		PushedAt:     clock.Now(),
		ValidatedAt:  clock.Now(),
	}))
	must(t, sd.WriteManifest(account, "foo", manifest.Digest.String(), manifest.Contents))
}

func expectBlobsExistInStorage(t *testing.T, sd keppel.StorageDriver, blobs ...keppel.Blob) {
	t.Helper()
	account := keppel.Account{Name: "test1"}
	for _, blob := range blobs {
		readCloser, sizeBytes, err := sd.ReadBlob(account, blob.StorageID)
		if err != nil {
			t.Errorf("expected blob %s to exist in the storage, but got: %s", blob.Digest, err.Error())
			continue
		}
		blobBytes, err := ioutil.ReadAll(readCloser)
		if err == nil {
			readCloser.Close()
		} else {
			err = readCloser.Close()
		}
		if err != nil {
			t.Errorf("unexpected error while reading blob %s: %s", blob.Digest, err.Error())
			continue
		}

		if uint64(len(blobBytes)) != sizeBytes {
			t.Errorf("unexpected error while reading blob %s: expected %d bytes, but got %d bytes", blob.Digest, sizeBytes, len(blobBytes))
			continue
		}

		expectedDigest, err := digest.Parse(blob.Digest)
		if err != nil {
			t.Errorf("blob digest %q is not a digest: %s", blob.Digest, err.Error())
			continue
		}
		actualDigest := expectedDigest.Algorithm().FromBytes(blobBytes)
		if actualDigest != expectedDigest {
			t.Errorf("blob %s has corrupted contents: actual digest is %s", blob.Digest, actualDigest)
			continue
		}
	}
}

func expectBlobsMissingInStorage(t *testing.T, sd keppel.StorageDriver, blobs ...keppel.Blob) {
	t.Helper()
	account := keppel.Account{Name: "test1"}
	for _, blob := range blobs {
		_, _, err := sd.ReadBlob(account, blob.StorageID)
		if err == nil {
			t.Errorf("expected blob %s to be missing in the storage, but could read it", blob.Digest)
			continue
		}
	}
}
