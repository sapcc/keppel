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
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

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

//ReplicateBlob replicates the given blob from its account's upstream registry.
//
//If a ResponseWriter is given, the response to the GET request to the upstream
//registry is also copied into it as the blob contents are being streamed into
//our local registry. The result value `responseWasWritten` indicates whether
//this happened. It may be false if an error occured before writing into the
//ResponseWriter took place.
func (r Replicator) ReplicateBlob(blob keppel.Blob, account keppel.Account, repo keppel.Repository, w http.ResponseWriter) (responseWasWritten bool, returnErr error) {
	//mark this blob as currently being replicated
	pendingBlob := keppel.PendingBlob{
		AccountName:  account.Name,
		Digest:       blob.Digest,
		Reason:       keppel.PendingBecauseOfReplication,
		PendingSince: time.Now(),
	}
	err := r.db.Insert(&pendingBlob)
	if err != nil {
		//did we get a duplicate-key error because this blob is already being replicated?
		count, err := r.db.SelectInt(
			`SELECT COUNT(*) FROM pending_blobs WHERE account_name = $1 AND digest = $2`,
			account.Name, blob.Digest,
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
			`DELETE FROM pending_blobs WHERE account_name = $1 AND digest = $2`,
			account.Name, blob.Digest,
		)
		if returnErr == nil {
			returnErr = err
		}
	}()

	//get a token for upstream
	var peer keppel.Peer
	err = r.db.SelectOne(&peer, `SELECT * FROM peers WHERE hostname = $1`, account.UpstreamPeerHostName)
	if err != nil {
		return false, err
	}
	peerToken, err := r.getPeerToken(peer, repo.FullName())
	if err != nil {
		return false, err
	}

	//query upstream for the blob
	blobReadCloser, blobLengthBytes, _, err := r.fetchFromUpstream(repo, "GET", "blobs/"+blob.Digest, peer, peerToken)
	if err != nil {
		return false, err
	}
	defer blobReadCloser.Close()

	//stream into `w` if requested
	blobReader := io.Reader(blobReadCloser)
	if w != nil {
		w.Header().Set("Docker-Content-Digest", blob.Digest)
		w.Header().Set("Content-Length", strconv.FormatUint(blobLengthBytes, 10))
		w.WriteHeader(http.StatusOK)
		blobReader = io.TeeReader(blobReader, w)
	}

	err = r.uploadBlobToLocal(blob, account, blobReader, blobLengthBytes)
	if err != nil {
		return true, err
	}

	//count the successful push
	l := prometheus.Labels{"account": account.Name, "method": "replication"}
	api.BlobsPushedCounter.With(l).Inc()
	return true, nil
}

const chunkSizeBytes = 500 << 20 // 500 MiB

func (r Replicator) uploadBlobToLocal(blob keppel.Blob, account keppel.Account, blobReader io.Reader, blobLengthBytes uint64) (returnErr error) {
	defer func() {
		//if blob upload fails, count an aborted upload
		if returnErr != nil {
			l := prometheus.Labels{"account": account.Name, "method": "replication"}
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

		err := r.sd.AppendToBlob(account, storageID, chunkCount, &chunkLength, chunk)
		if err != nil {
			abortErr := r.sd.AbortBlobUpload(account, storageID, chunkCount)
			if abortErr != nil {
				logg.Error("additional error encountered when aborting upload %s into account %s: %s",
					storageID, account.Name, abortErr.Error())
			}
			return err
		}

		remainingBytes -= chunkLength
	}

	err := r.sd.FinalizeBlob(account, storageID, chunkCount)
	if err != nil {
		abortErr := r.sd.AbortBlobUpload(account, storageID, chunkCount)
		if abortErr != nil {
			logg.Error("additional error encountered when aborting upload %s into account %s: %s",
				storageID, account.Name, abortErr.Error())
		}
		return err
	}

	//if errors occur while trying to update the DB, we need to clean up the blob in the storage
	defer func() {
		if returnErr != nil {
			deleteErr := r.sd.DeleteBlob(account, storageID)
			if deleteErr != nil {
				logg.Error("additional error encountered when deleting uploaded blob %s from account %s after upload error: %s",
					storageID, account.Name, deleteErr.Error())
			}
		}
	}()

	//write blob metadata to DB
	blob.StorageID = storageID
	blob.PushedAt = time.Now()
	blob.ValidatedAt = blob.PushedAt
	_, err = r.db.Update(&blob)
	return err
}
