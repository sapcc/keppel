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
	"io/ioutil"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/sapcc/keppel/internal/clair"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
	"gopkg.in/gorp.v2"
)

func setup(t *testing.T) (*Janitor, test.Setup) {
	s := test.NewSetup(t,
		test.WithPeerAPI,
		test.WithAccount(keppel.Account{Name: "test1", AuthTenantID: "test1authtenant"}),
		test.WithRepo(keppel.Repository{AccountName: "test1", Name: "foo"}),
	)
	j := NewJanitor(s.Config, s.FD, s.SD, s.ICD, s.DB, s.Auditor).OverrideTimeNow(s.Clock.Now).OverrideGenerateStorageID(s.SIDGenerator.Next)
	return j, s
}

func forAllReplicaTypes(t *testing.T, action func(string)) {
	action("on_first_use")
	action("from_external_on_first_use")
}

func setupReplica(t *testing.T, s1 test.Setup, strategy string) (*Janitor, test.Setup) {
	testAccount := keppel.Account{
		Name:         "test1",
		AuthTenantID: "test1authtenant",
	}
	switch strategy {
	case "on_first_use":
		testAccount.UpstreamPeerHostName = "registry.example.org"
	case "from_external_on_first_use":
		testAccount.ExternalPeerURL = "registry.example.org/test1"
		testAccount.ExternalPeerUserName = "replication@registry-secondary.example.org"
		testAccount.ExternalPeerPassword = test.ReplicationPassword
	default:
		t.Fatalf("unknown strategy: %q", strategy)
	}

	s := test.NewSetup(t,
		test.IsSecondaryTo(&s1),
		test.WithPeerAPI,
		test.WithAccount(testAccount),
		test.WithRepo(keppel.Repository{AccountName: "test1", Name: "foo"}),
	)

	j2 := NewJanitor(s.Config, s.FD, s.SD, s.ICD, s.DB, s.Auditor).OverrideTimeNow(s.Clock.Now).OverrideGenerateStorageID(s.SIDGenerator.Next)
	return j2, s
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

func uploadBlob(t *testing.T, s test.Setup, blob test.Bytes) keppel.Blob {
	t.Helper()
	account := keppel.Account{Name: "test1"}
	repo := keppel.Repository{ID: 1, Name: "foo", AccountName: "test1"}

	//get a storage ID deterministically
	storageID := blob.Digest.Encoded()

	dbBlob := keppel.Blob{
		AccountName: "test1",
		Digest:      blob.Digest.String(),
		SizeBytes:   uint64(len(blob.Contents)),
		StorageID:   storageID,
		PushedAt:    s.Clock.Now(),
		ValidatedAt: s.Clock.Now(),
		MediaType:   blob.MediaType,
	}
	must(t, s.DB.Insert(&dbBlob))
	must(t, s.SD.AppendToBlob(account, storageID, 1, &dbBlob.SizeBytes, bytes.NewBuffer(blob.Contents)))
	must(t, s.SD.FinalizeBlob(account, storageID, 1))
	must(t, keppel.MountBlobIntoRepo(s.DB, dbBlob, repo))
	return dbBlob
}

func uploadManifest(t *testing.T, s test.Setup, manifest test.Bytes, sizeBytes uint64) keppel.Manifest {
	t.Helper()
	account := keppel.Account{Name: "test1"}

	dbManifest := keppel.Manifest{
		RepositoryID:        1,
		Digest:              manifest.Digest.String(),
		MediaType:           manifest.MediaType,
		SizeBytes:           sizeBytes,
		PushedAt:            s.Clock.Now(),
		ValidatedAt:         s.Clock.Now(),
		VulnerabilityStatus: clair.PendingVulnerabilityStatus,
	}
	must(t, s.DB.Insert(&dbManifest))
	must(t, s.DB.Insert(&keppel.ManifestContent{
		RepositoryID: 1,
		Digest:       manifest.Digest.String(),
		Content:      manifest.Contents,
	}))
	must(t, s.SD.WriteManifest(account, "foo", manifest.Digest.String(), manifest.Contents))
	return dbManifest
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

func expectManifestsExistInStorage(t *testing.T, sd keppel.StorageDriver, manifests ...keppel.Manifest) {
	t.Helper()
	account := keppel.Account{Name: "test1"}
	for _, manifest := range manifests {
		manifestBytes, err := sd.ReadManifest(account, "foo", manifest.Digest)
		if err != nil {
			t.Errorf("expected manifest %s to exist in the storage, but got: %s", manifest.Digest, err.Error())
			continue
		}
		expectedDigest, err := digest.Parse(manifest.Digest)
		if err != nil {
			t.Errorf("manifest digest %q is not a digest: %s", manifest.Digest, err.Error())
			continue
		}
		actualDigest := expectedDigest.Algorithm().FromBytes(manifestBytes)
		if actualDigest != expectedDigest {
			t.Errorf("manifest %s has corrupted contents: actual digest is %s", manifest.Digest, actualDigest)
			continue
		}
	}
}

func expectManifestsMissingInStorage(t *testing.T, sd keppel.StorageDriver, manifests ...keppel.Manifest) {
	t.Helper()
	account := keppel.Account{Name: "test1"}
	for _, manifest := range manifests {
		_, err := sd.ReadManifest(account, "foo", manifest.Digest)
		if err == nil {
			t.Errorf("expected manifest %s to be missing in the storage, but could read it", manifest.Digest)
			continue
		}
	}
}
