/*******************************************************************************
*
* Copyright 2020 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package processor

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/docker/distribution"
	"github.com/opencontainers/go-digest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/api"
	"github.com/sapcc/keppel/internal/keppel"
	"gopkg.in/gorp.v2"
)

//ValidateExistingBlob validates the given blob that already exists in the DB.
//Validation includes computing the digest of the blob contents and comparing
//to the digest in the DB. On success, nil is returned.
func (p *Processor) ValidateExistingBlob(account keppel.Account, blob keppel.Blob) (returnErr error) {
	blobDigest, err := digest.Parse(blob.Digest)
	if err != nil {
		return fmt.Errorf("cannot parse blob digest: %s", err.Error())
	}

	readCloser, _, err := p.sd.ReadBlob(account, blob.StorageID)
	if err != nil {
		return err
	}
	defer func() {
		if err == nil {
			err = readCloser.Close()
		} else {
			readCloser.Close()
		}
	}()

	bcw := &byteCountingWriter{}
	reader := io.TeeReader(readCloser, bcw)

	actualDigest, err := blobDigest.Algorithm().FromReader(reader)
	if err != nil {
		return err
	}
	if actualDigest != blobDigest {
		return fmt.Errorf("expected digest %s, but got %s",
			blob.Digest, actualDigest.String(),
		)
	}

	if uint64(bcw.bytesWritten) != blob.SizeBytes {
		return fmt.Errorf("expected %d bytes, but got %d bytes",
			blob.SizeBytes, bcw.bytesWritten,
		)
	}

	return nil
}

//An io.Writer that just counts how many bytes were written into it.
type byteCountingWriter struct {
	bytesWritten int
}

func (w *byteCountingWriter) Write(buf []byte) (int, error) {
	w.bytesWritten += len(buf)
	return len(buf), nil
}

//FindBlobOrInsertUnbackedBlob is used by the replication code path. If the
//requested blob does not exist, a blob record with an empty storage ID will be
//inserted into the DB. This indicates to the registry API handler that this
//blob shall be replicated when it is first pulled.
func (p *Processor) FindBlobOrInsertUnbackedBlob(desc distribution.Descriptor, account keppel.Account) (*keppel.Blob, error) {
	var blob *keppel.Blob
	err := p.insideTransaction(func(tx *gorp.Transaction) error {
		var err error
		blob, err = keppel.FindBlobByAccountName(tx, desc.Digest, account)
		if err != sql.ErrNoRows { //either success or unexpected error
			return err
		}

		blob = &keppel.Blob{
			AccountName: account.Name,
			Digest:      desc.Digest.String(),
			SizeBytes:   uint64(desc.Size),
			StorageID:   "", //unbacked
			PushedAt:    time.Unix(0, 0),
			ValidatedAt: time.Unix(0, 0),
		}
		return tx.Insert(blob)
	})
	return blob, err
}

var (
	//ErrConcurrentReplication is returned from Processor.ReplicateBlob() when the
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
func (p *Processor) ReplicateBlob(blob keppel.Blob, account keppel.Account, repo keppel.Repository, w http.ResponseWriter) (responseWasWritten bool, returnErr error) {
	//mark this blob as currently being replicated
	pendingBlob := keppel.PendingBlob{
		AccountName:  account.Name,
		Digest:       blob.Digest,
		Reason:       keppel.PendingBecauseOfReplication,
		PendingSince: p.timeNow(),
	}
	err := p.db.Insert(&pendingBlob)
	if err != nil {
		//did we get a duplicate-key error because this blob is already being replicated?
		count, err := p.db.SelectInt(
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
		_, err := p.db.Exec(
			`DELETE FROM pending_blobs WHERE account_name = $1 AND digest = $2`,
			account.Name, blob.Digest,
		)
		if returnErr == nil {
			returnErr = err
		}
	}()

	//query upstream for the blob
	client, err := p.getRepoClientForUpstream(account, repo)
	if err != nil {
		return false, err
	}
	blobReadCloser, blobLengthBytes, err := client.DownloadBlob(digest.Digest(blob.Digest))
	if err != nil {
		return false, err
	}
	defer blobReadCloser.Close()

	//stream into `w` if requested
	blobReader := io.Reader(blobReadCloser)
	if w != nil {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Docker-Content-Digest", blob.Digest)
		w.Header().Set("Content-Length", strconv.FormatUint(blobLengthBytes, 10))
		w.WriteHeader(http.StatusOK)
		blobReader = io.TeeReader(blobReader, w)
	}

	err = p.uploadBlobToLocal(blob, account, blobReader, blobLengthBytes)
	if err != nil {
		return true, err
	}

	//count the successful push
	l := prometheus.Labels{"account": account.Name, "auth_tenant_id": account.AuthTenantID, "method": "replication"}
	api.BlobsPushedCounter.With(l).Inc()
	return true, nil
}

func (p *Processor) uploadBlobToLocal(blob keppel.Blob, account keppel.Account, blobReader io.Reader, blobLengthBytes uint64) (returnErr error) {
	defer func() {
		//if blob upload fails, count an aborted upload
		if returnErr != nil {
			l := prometheus.Labels{"account": account.Name, "auth_tenant_id": account.AuthTenantID, "method": "replication"}
			api.UploadsAbortedCounter.With(l).Inc()
		}
	}()

	upload := keppel.Upload{
		StorageID: p.generateStorageID(),
		SizeBytes: 0,
		NumChunks: 0,
	}
	err := p.AppendToBlob(account, &upload, blobReader, blobLengthBytes)
	if err != nil {
		abortErr := p.sd.AbortBlobUpload(account, upload.StorageID, upload.NumChunks)
		if abortErr != nil {
			logg.Error("additional error encountered when aborting upload %s into account %s: %s",
				upload.StorageID, account.Name, abortErr.Error())
		}
		return err
	}

	err = p.sd.FinalizeBlob(account, upload.StorageID, upload.NumChunks)
	if err != nil {
		abortErr := p.sd.AbortBlobUpload(account, upload.StorageID, upload.NumChunks)
		if abortErr != nil {
			logg.Error("additional error encountered when aborting upload %s into account %s: %s",
				upload.StorageID, account.Name, abortErr.Error())
		}
		return err
	}

	//if errors occur while trying to update the DB, we need to clean up the blob in the storage
	defer func() {
		if returnErr != nil {
			deleteErr := p.sd.DeleteBlob(account, upload.StorageID)
			if deleteErr != nil {
				logg.Error("additional error encountered when deleting uploaded blob %s from account %s after upload error: %s",
					upload.StorageID, account.Name, deleteErr.Error())
			}
		}
	}()

	//write blob metadata to DB
	blob.StorageID = upload.StorageID
	blob.PushedAt = p.timeNow()
	blob.ValidatedAt = blob.PushedAt
	_, err = p.db.Update(&blob)
	return err
}

//AppendToBlob appends bytes to a blob upload, and updates the upload's
//SizeBytes and NumChunks fields appropriately. Chunking of large uploads is
//implemented at this level, to accomodate storage drivers that have a size
//restriction on blob chunks.
//
//Warning: The upload's Digest field is *not* read or written. For chunked
//uploads, the caller is responsible for performing and validating the digest
//computation.
func (p *Processor) AppendToBlob(account keppel.Account, upload *keppel.Upload, contents io.Reader, lengthBytes uint64) error {
	return foreachChunk(contents, lengthBytes, func(chunk io.Reader, chunkLengthBytes uint64) error {
		upload.NumChunks++
		upload.SizeBytes += chunkLengthBytes
		return p.sd.AppendToBlob(account, upload.StorageID, upload.NumChunks, &chunkLengthBytes, chunk)
	})
}

const chunkSizeBytes = 500 << 20 // 500 MiB

//This function contains the logic for splitting `contents` (containing `lengthBytes`) into chunks of `chunkSizeBytes` max.
func foreachChunk(contents io.Reader, lengthBytes uint64, action func(io.Reader, uint64) error) error {
	//NOTE: This function is written such that `action` is called at least once,
	//even when `contents` is empty.
	remainingBytes := lengthBytes
	for remainingBytes > chunkSizeBytes {
		err := action(io.LimitReader(contents, chunkSizeBytes), chunkSizeBytes)
		if err != nil {
			return err
		}
		remainingBytes -= chunkSizeBytes
	}
	return action(contents, remainingBytes)
}
