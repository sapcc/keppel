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
	"net/http"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/sapcc/keppel/internal/api"
	authapi "github.com/sapcc/keppel/internal/api/auth"
	registryv2 "github.com/sapcc/keppel/internal/api/registry"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/gorp.v2"
)

//these credentials are in global vars so that we don't have to recompute them
//in every test run (bcrypt is intentionally CPU-intensive)
var (
	replicationPassword     string
	replicationPasswordHash string
)

func setup(t *testing.T) (*Janitor, keppel.Configuration, *keppel.DB, *test.FederationDriver, keppel.StorageDriver, *test.Clock, http.Handler) {
	cfg, db := test.Setup(t)

	ad, err := keppel.NewAuthDriver("unittest", nil)
	must(t, err)
	fd, err := keppel.NewFederationDriver("unittest", ad, cfg)
	must(t, err)
	sd, err := keppel.NewStorageDriver("in-memory-for-testing", ad, cfg)
	must(t, err)

	must(t, db.Insert(&keppel.Account{Name: "test1", AuthTenantID: "test1authtenant"}))
	must(t, db.Insert(&keppel.Repository{AccountName: "test1", Name: "foo"}))

	clock := &test.Clock{}
	sidGen := &test.StorageIDGenerator{}
	j := NewJanitor(cfg, fd, sd, db).OverrideTimeNow(clock.Now).OverrideGenerateStorageID(sidGen.Next)

	h := api.Compose(
		registryv2.NewAPI(cfg, sd, db, nil).OverrideTimeNow(clock.Now).OverrideGenerateStorageID(sidGen.Next),
		authapi.NewAPI(cfg, ad, db),
	)

	return j, cfg, db, fd.(*test.FederationDriver), sd, clock, h
}

func setupReplica(t *testing.T, db1 *keppel.DB, h1 http.Handler, clock *test.Clock) (*Janitor, keppel.Configuration, *keppel.DB, keppel.StorageDriver, http.Handler) {
	cfg2, db2 := test.SetupSecondary(t)

	ad2, err := keppel.NewAuthDriver("unittest", nil)
	must(t, err)
	fd2, err := keppel.NewFederationDriver("unittest", ad2, cfg2)
	must(t, err)
	sd2, err := keppel.NewStorageDriver("in-memory-for-testing", ad2, cfg2)
	must(t, err)

	must(t, db2.Insert(&keppel.Account{Name: "test1", AuthTenantID: "test1authtenant", UpstreamPeerHostName: "registry.example.org"}))
	must(t, db2.Insert(&keppel.Repository{AccountName: "test1", Name: "foo"}))

	//give the secondary registry credentials for replicating from the primary
	if replicationPassword == "" {
		//this password needs to be constant because it appears in some fixtures/*.sql
		replicationPassword = "a4cb6fae5b8bb91b0b993486937103dab05eca93"

		hashBytes, _ := bcrypt.GenerateFromPassword([]byte(replicationPassword), 8)
		replicationPasswordHash = string(hashBytes)
	}

	must(t, db2.Insert(&keppel.Peer{
		HostName:    "registry.example.org",
		OurPassword: replicationPassword,
	}))
	must(t, db1.Insert(&keppel.Peer{
		HostName:                 "registry-secondary.example.org",
		TheirCurrentPasswordHash: replicationPasswordHash,
	}))

	sidGen := &test.StorageIDGenerator{}
	j2 := NewJanitor(cfg2, fd2, sd2, db2).OverrideTimeNow(clock.Now).OverrideGenerateStorageID(sidGen.Next)
	h2 := api.Compose(
		registryv2.NewAPI(cfg2, sd2, db2, nil).OverrideTimeNow(clock.Now).OverrideGenerateStorageID(sidGen.Next),
		authapi.NewAPI(cfg2, ad2, db2),
	)

	//the secondary registry wants to talk to the primary registry over HTTPS, so
	//attach the primary registry's HTTP handler to the http.DefaultClient
	tt := &test.RoundTripper{
		Handlers: map[string]http.Handler{
			"registry.example.org":           h1,
			"registry-secondary.example.org": h2,
		},
	}
	http.DefaultClient.Transport = tt

	return j2, cfg2, db2, sd2, h2
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
	storageID := blob.Digest.Encoded()

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

func uploadManifest(t *testing.T, db *keppel.DB, sd keppel.StorageDriver, clock *test.Clock, manifest test.Bytes, sizeBytes uint64) keppel.Manifest {
	t.Helper()
	account := keppel.Account{Name: "test1"}

	dbManifest := keppel.Manifest{
		RepositoryID: 1,
		Digest:       manifest.Digest.String(),
		MediaType:    manifest.MediaType,
		SizeBytes:    sizeBytes,
		PushedAt:     clock.Now(),
		ValidatedAt:  clock.Now(),
	}
	must(t, db.Insert(&dbManifest))
	must(t, sd.WriteManifest(account, "foo", manifest.Digest.String(), manifest.Contents))
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
