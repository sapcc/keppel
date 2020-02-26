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

package replication

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/api"
	"github.com/sapcc/keppel/internal/keppel"
)

var (
	//ErrConcurrentReplication is returned from BlobRequest.Execute() when the
	//same blob is already being replicated by another worker.
	ErrConcurrentReplication = errors.New("currently replicating")
)

//Blob describes a blob that can be replicated into our local registry.
type Blob struct {
	Account keppel.Account
	Repo    keppel.Repository
	Digest  digest.Digest
}

//ReplicateBlob replicates the given blob from its account's upstream registry.
//
//If a ResponseWriter is given, the response to the GET request to the upstream
//registry is also copied into it as the blob contents are being streamed into
//our local registry. The result value `responseWasWritten` indicates whether
//this happened. It may be false if an error occured before writing into the
//ResponseWriter took place.
//
//`requestMethod` is usually "GET", but can also be set to "HEAD". In this
//case, no replication will take place. The upstream HEAD request will just be
//proxied into the given ResponseWriter.
func (r Replicator) ReplicateBlob(b Blob, w http.ResponseWriter, requestMethod string) (responseWasWritten bool, returnErr error) {
	//mark this blob as currently being replicated
	pendingBlob := keppel.PendingBlob{
		RepositoryID: b.Repo.ID,
		Digest:       b.Digest.String(),
		Reason:       keppel.PendingBecauseOfReplication,
		PendingSince: time.Now(),
	}
	err := r.db.Insert(&pendingBlob)
	if err != nil {
		//did we get a duplicate-key error because this blob is already being replicated?
		count, err := r.db.SelectInt(
			`SELECT COUNT(*) FROM pending_blobs WHERE repo_id = $1 AND digest = $2`,
			b.Repo.ID, b.Digest.String(),
		)
		if err == nil && count > 0 {
			return false, ErrConcurrentReplication
		}
		return false, err
	}

	//whatever happens, don't forget to cleanup the PendingBlob DB entry afterwards
	//to unblock others who are waiting for this replication to come to an end
	//(one way or the other)
	defer func() {
		_, err := r.db.Exec(
			`DELETE FROM pending_blobs WHERE repo_id = $1 AND digest = $2`,
			b.Repo.ID, b.Digest.String(),
		)
		if returnErr == nil {
			returnErr = err
		}
	}()

	//if we already have the same blob locally in a different repo, we would not
	//need to transfer its contents again, we could just mount it
	localBlob, err := r.db.FindBlobByAccountName(b.Digest, b.Account)
	if err == sql.ErrNoRows {
		localBlob = nil
	} else if err != nil {
		return false, err
	}

	//get a token for upstream
	var peer keppel.Peer
	err = r.db.SelectOne(&peer, `SELECT * FROM peers WHERE hostname = $1`, b.Account.UpstreamPeerHostName)
	if err != nil {
		return false, err
	}
	peerToken, err := r.getPeerToken(peer, b.Repo.FullName())
	if err != nil {
		return false, err
	}

	//query upstream for the blob
	upstreamMethod := "GET"
	if requestMethod == "HEAD" || localBlob != nil {
		upstreamMethod = "HEAD"
	}
	blobReadCloser, blobLengthBytes, _, err := r.fetchFromUpstream(b.Repo, upstreamMethod, "blobs/"+b.Digest.String(), peer, peerToken)
	if err != nil {
		return false, err
	}
	defer blobReadCloser.Close()

	//at this point, it's clear that upstream has the blob; if we also have it
	//locally, we just need to mount it; then we can revert to the regular code path
	if localBlob != nil {
		return false, keppel.MountBlobIntoRepo(r.db, *localBlob, b.Repo)
	}

	//stream into `w` if requested
	blobReader := io.Reader(blobReadCloser)
	if w != nil {
		w.Header().Set("Docker-Content-Digest", b.Digest.String())
		w.Header().Set("Content-Length", strconv.FormatUint(blobLengthBytes, 10))
		w.WriteHeader(http.StatusOK)
		blobReader = io.TeeReader(blobReader, w)
	}

	//upload into local keppel-registry if we have a blob content to upload
	if requestMethod == "HEAD" {
		return true, nil
	}

	err = r.uploadBlobToLocal(b, blobReader, blobLengthBytes)
	if err != nil {
		return true, err
	}

	//count the successful push
	l := prometheus.Labels{"account": b.Account.Name, "method": "replication"}
	api.BlobsPushedCounter.With(l).Inc()
	return true, nil
}

const chunkSizeBytes = 500 << 20 // 500 MiB

func (r Replicator) uploadBlobToLocal(b Blob, blobReader io.Reader, blobLengthBytes uint64) (returnErr error) {
	defer func() {
		//if blob upload fails, count an aborted upload
		if returnErr != nil {
			l := prometheus.Labels{"account": b.Account.Name, "method": "replication"}
			api.UploadsAbortedCounter.With(l).Inc()
		}
	}()

	chunkCount := uint32(0)
	remainingBytes := blobLengthBytes
	storageID := keppel.GenerateStorageID()

	for chunkCount == 0 || remainingBytes > 0 {
		var (
			chunk       io.Reader
			chunkLength uint64
		)
		if remainingBytes > chunkSizeBytes {
			chunk = io.LimitReader(blobReader, chunkSizeBytes)
			chunkLength = chunkSizeBytes
		} else {
			chunk = blobReader
			chunkLength = remainingBytes
		}
		chunkCount++

		err := r.sd.AppendToBlob(b.Account, storageID, chunkCount, &chunkLength, chunk)
		if err != nil {
			abortErr := r.sd.AbortBlobUpload(b.Account, storageID, chunkCount)
			if abortErr != nil {
				logg.Error("additional error encountered when aborting upload %s into account %s: %s",
					storageID, b.Account.Name, abortErr.Error())
			}
			return err
		}

		remainingBytes -= chunkLength
	}

	err := r.sd.FinalizeBlob(b.Account, storageID, chunkCount)
	if err != nil {
		abortErr := r.sd.AbortBlobUpload(b.Account, storageID, chunkCount)
		if abortErr != nil {
			logg.Error("additional error encountered when aborting upload %s into account %s: %s",
				storageID, b.Account.Name, abortErr.Error())
		}
		return err
	}

	//if errors occur while trying to update the DB, we need to clean up the blob in the storage
	defer func() {
		if returnErr != nil {
			deleteErr := r.sd.DeleteBlob(b.Account, storageID)
			if deleteErr != nil {
				logg.Error("additional error encountered when deleting uploaded blob %s from account %s after upload error: %s",
					storageID, b.Account.Name, deleteErr.Error())
			}
		}
	}()

	//write blob metadata to DB
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer keppel.RollbackUnlessCommitted(tx)
	dbBlob := keppel.Blob{
		AccountName: b.Account.Name,
		Digest:      b.Digest.String(),
		SizeBytes:   blobLengthBytes,
		StorageID:   storageID,
		PushedAt:    time.Now(),
	}
	err = tx.Insert(&dbBlob)
	if err != nil {
		return err
	}
	err = keppel.MountBlobIntoRepo(tx, dbBlob, b.Repo)
	if err != nil {
		return err
	}
	return tx.Commit()
}
