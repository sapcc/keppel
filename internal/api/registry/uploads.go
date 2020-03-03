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

package registryv2

import (
	"crypto/sha256"
	"database/sql"
	"encoding"
	"encoding/base64"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/opencontainers/go-digest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sre"
	"github.com/sapcc/keppel/internal/api"
	"github.com/sapcc/keppel/internal/keppel"
	uuid "github.com/satori/go.uuid"
	"gopkg.in/gorp.v2"
)

//This implements the POST /v2/<account>/<repository>/blobs/uploads/ endpoint.
func (a *API) handleStartBlobUpload(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/v2/:account/:repo/blobs/uploads/")
	account, repoName, _ := a.checkAccountAccess(w, r)
	if account == nil {
		return
	}

	//forbid pushing into replica accounts
	if account.UpstreamPeerHostName != "" {
		msg := fmt.Sprintf("cannot push into replica account (push to %s/%s/%s instead!)",
			account.UpstreamPeerHostName, account.Name, repoName,
		)
		keppel.ErrUnsupported.With(msg).WithStatus(http.StatusMethodNotAllowed).WriteAsRegistryV2ResponseTo(w)
		return
	}

	//only allow new blob uploads when there is enough quota to push a manifest
	//
	//This is not strictly necessary to enforce the manifest quota, but it's
	//useful to avoid the accumulation of unreferenced blobs in the account's
	//backing storage.
	quotas, err := a.db.FindQuotas(account.AuthTenantID)
	if respondWithError(w, err) {
		return
	}
	if quotas == nil {
		quotas = keppel.DefaultQuotas(account.AuthTenantID)
	}
	manifestUsage, err := quotas.GetManifestUsage(a.db)
	if respondWithError(w, err) {
		return
	}
	if manifestUsage >= quotas.ManifestCount {
		msg := fmt.Sprintf("manifest quota exceeded (quota = %d, usage = %d)",
			quotas.ManifestCount, manifestUsage,
		)
		keppel.ErrDenied.With(msg).WithStatus(http.StatusConflict).WriteAsRegistryV2ResponseTo(w)
		return
	}

	repo, err := a.db.FindOrCreateRepository(repoName, *account)
	if respondWithError(w, err) {
		return
	}

	//special case: request for cross-repo blob mount
	query := r.URL.Query()
	if sourceRepoFullName := query.Get("from"); sourceRepoFullName != "" {
		a.performCrossRepositoryBlobMount(w, r, *account, *repo, sourceRepoFullName, query.Get("mount"))
		return
	}

	//special case: monolithic upload
	if blobDigestStr := query.Get("digest"); blobDigestStr != "" {
		a.performMonolithicUpload(w, r, *account, *repo, blobDigestStr)
		return
	}

	//start a new upload
	upload := keppel.Upload{
		RepositoryID: repo.ID,
		UUID:         uuid.NewV4().String(),
		StorageID:    a.generateStorageID(),
		SizeBytes:    0,
		Digest:       "",
		NumChunks:    0,
		UpdatedAt:    a.timeNow(),
	}

	err = a.db.Insert(&upload)
	if respondWithError(w, err) {
		return
	}

	w.Header().Set("Blob-Upload-Session-Id", upload.UUID)
	w.Header().Set("Content-Length", "0")
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s", repo.FullName(), upload.UUID))
	w.Header().Set("Range", "0-0")
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) performCrossRepositoryBlobMount(w http.ResponseWriter, r *http.Request, account keppel.Account, targetRepo keppel.Repository, sourceRepoFullName, blobDigestStr string) {
	//validate source repository
	if !strings.HasPrefix(sourceRepoFullName, account.Name+"/") {
		keppel.ErrUnsupported.With("cannot mount blobs across different accounts").WriteAsRegistryV2ResponseTo(w)
		return
	}
	sourceRepoName := strings.TrimPrefix(sourceRepoFullName, account.Name+"/")
	if !repoNameWithLeadingSlashRx.MatchString("/" + sourceRepoName) {
		keppel.ErrNameInvalid.With("source repository is invalid").WriteAsRegistryV2ResponseTo(w)
		return
	}
	sourceRepo, err := a.db.FindRepository(sourceRepoName, account)
	if err == sql.ErrNoRows {
		keppel.ErrNameUnknown.With("source repository does not exist").WriteAsRegistryV2ResponseTo(w)
		return
	}
	if respondWithError(w, err) {
		return
	}

	//validate blob
	blobDigest, err := digest.Parse(blobDigestStr)
	if err != nil {
		keppel.ErrDigestInvalid.With(err.Error()).WriteAsRegistryV2ResponseTo(w)
		return
	}
	blob, err := keppel.FindBlobByRepositoryID(a.db, blobDigest, sourceRepo.ID, account)
	if err == sql.ErrNoRows {
		keppel.ErrBlobUnknown.With("blob does not exist in source repository").WriteAsRegistryV2ResponseTo(w)
		return
	}
	if respondWithError(w, err) {
		return
	}

	//create blob mount if missing
	err = keppel.MountBlobIntoRepo(a.db, *blob, targetRepo)
	if respondWithError(w, err) {
		return
	}

	//the spec wants a Blob-Upload-Session-Id header even though the upload is done, so just make something up
	w.Header().Set("Blob-Upload-Session-Id", uuid.NewV4().String())
	w.Header().Set("Content-Length", "0")
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/%s", targetRepo.FullName(), blobDigest.String()))
	w.WriteHeader(http.StatusCreated)
}

func (a *API) performMonolithicUpload(w http.ResponseWriter, r *http.Request, account keppel.Account, repo keppel.Repository, blobDigestStr string) (ok bool) {
	blobDigest, err := digest.Parse(blobDigestStr)
	if err != nil {
		keppel.ErrDigestInvalid.With(err.Error()).WriteAsRegistryV2ResponseTo(w)
		return false
	}

	//parse Content-Length
	sizeBytesStr := r.Header.Get("Content-Length")
	if sizeBytesStr == "" {
		keppel.ErrSizeInvalid.With("missing Content-Length header").WriteAsRegistryV2ResponseTo(w)
		return false
	}
	sizeBytes, err := strconv.ParseUint(sizeBytesStr, 10, 64)
	if sizeBytesStr == "" {
		//COVERAGE: unreachable in unit tests because net/http validates Content-Length header format before sending
		keppel.ErrSizeInvalid.With("invalid Content-Length: " + err.Error()).WriteAsRegistryV2ResponseTo(w)
		return false
	}

	//stream request body into the storage backend while also computing the digest and length
	storageID := a.generateStorageID()
	dw := digestWriter{Hash: sha256.New()}
	err = a.sd.AppendToBlob(account, storageID, 1, &sizeBytes, io.TeeReader(r.Body, &dw))
	if respondWithError(w, err) {
		return false
	}
	err = a.sd.FinalizeBlob(account, storageID, 1)
	if respondWithError(w, err) {
		countAbortedBlobUpload(account)
		err := a.sd.AbortBlobUpload(account, storageID, 1)
		if err != nil {
			logg.Error("additional error encountered while aborting blob upload %s into %s: %s", storageID, repo.FullName(), err.Error())
		}
		return false
	}

	//if any of the remaining steps fail, don't forget to cleanup the storage backend
	defer func() {
		if !ok {
			countAbortedBlobUpload(account)
			err := a.sd.DeleteBlob(account, storageID)
			if err != nil {
				logg.Error("additional error encountered while deleting broken blob %s from %s: %s", storageID, repo.FullName(), err.Error())
			}
			return
		}
	}()

	//validate digest and length
	if dw.bytesWritten != sizeBytes {
		keppel.ErrSizeInvalid.With("Content-Length was %d, but %d bytes were sent", sizeBytes, dw.bytesWritten).WriteAsRegistryV2ResponseTo(w)
		return false
	}

	actualDigest := digest.NewDigest(digest.SHA256, dw.Hash)
	if actualDigest != blobDigest {
		keppel.ErrDigestInvalid.With("expected %s, but actual digest was %s", blobDigestStr, actualDigest.String()).WriteAsRegistryV2ResponseTo(w)
		return false
	}

	//record blob in DB
	tx, err := a.db.Begin()
	if respondWithError(w, err) {
		return false
	}
	defer keppel.RollbackUnlessCommitted(tx)

	blobPushedAt := a.timeNow()
	blob := keppel.Blob{
		AccountName: account.Name,
		Digest:      blobDigest.String(),
		SizeBytes:   sizeBytes,
		StorageID:   storageID,
		PushedAt:    blobPushedAt,
		ValidatedAt: blobPushedAt,
	}
	onCommit, err := a.createOrUpdateBlobObject(tx, &blob, account)
	if respondWithError(w, err) {
		return false
	}
	err = keppel.MountBlobIntoRepo(tx, blob, repo)
	if respondWithError(w, err) {
		return false
	}
	err = tx.Commit()
	if respondWithError(w, err) {
		return false
	}
	if onCommit != nil {
		onCommit()
	}

	//the spec wants a Blob-Upload-Session-Id header even though the upload is done, so just make something up
	w.Header().Set("Blob-Upload-Session-Id", uuid.NewV4().String())
	w.Header().Set("Content-Length", "0")
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/%s", repo.FullName(), blobDigest.String()))
	w.WriteHeader(http.StatusCreated)
	return true
}

//This implements the DELETE /v2/<account>/<repository>/blobs/uploads/<uuid> endpoint.
func (a *API) handleDeleteBlobUpload(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/v2/:account/:repo/blobs/uploads/:uuid")
	account, repoName, _ := a.checkAccountAccess(w, r)
	if account == nil {
		return
	}
	upload := a.findUpload(w, r, *account, repoName)
	if upload == nil {
		return
	}

	//prepare the database transaction for deleting this upload
	tx, err := a.db.Begin()
	if respondWithError(w, err) {
		return
	}
	defer keppel.RollbackUnlessCommitted(tx)
	_, err = tx.Delete(upload)
	if respondWithError(w, err) {
		return
	}

	//perform the deletion in the storage backend, then make the DB change durable
	if upload.NumChunks > 0 {
		err = a.sd.AbortBlobUpload(*account, upload.StorageID, upload.NumChunks)
		if respondWithError(w, err) {
			return
		}
	}
	err = tx.Commit()
	if respondWithError(w, err) {
		return
	}

	//report success
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusNoContent)
}

//This implements the GET /v2/<account>/<repository>/blobs/uploads/<uuid> endpoint.
func (a *API) handleGetBlobUpload(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/v2/:account/:repo/blobs/uploads/:uuid")
	account, repoName, _ := a.checkAccountAccess(w, r)
	if account == nil {
		return
	}
	upload := a.findUpload(w, r, *account, repoName)
	if upload == nil {
		return
	}

	w.Header().Set("Blob-Upload-Session-Id", upload.UUID)
	w.Header().Set("Content-Length", "0")
	w.Header().Set("Range", fmt.Sprintf("0-%d", upload.SizeBytes))
	w.WriteHeader(http.StatusNoContent)
}

//This implements the PATCH /v2/<account>/<repository>/blobs/uploads/<uuid> endpoint.
func (a *API) handleContinueBlobUpload(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/v2/:account/:repo/blobs/uploads/:uuid")
	account, repoName, _ := a.checkAccountAccess(w, r)
	if account == nil {
		return
	}
	upload := a.findUpload(w, r, *account, repoName)
	if upload == nil {
		return
	}
	dw, rerr := a.resumeUpload(*account, upload, r.URL.Query().Get("state"))
	if rerr != nil {
		rerr.WriteAsRegistryV2ResponseTo(w)
		return
	}

	//if we have the Content-Range and Content-Length headers ("chunked upload mode"),
	//parse and validate them
	chunkSizeBytes := (*uint64)(nil)
	if r.Header.Get("Content-Range") != "" {
		val, err := a.parseContentRange(upload, r.Header)
		if err != nil {
			keppel.ErrSizeInvalid.With(err.Error()).WriteAsRegistryV2ResponseTo(w)

			logg.Info("aborting upload because of error during parseContentRange()")
			countAbortedBlobUpload(*account)
			err := a.sd.AbortBlobUpload(*account, upload.StorageID, upload.NumChunks)
			if err != nil {
				logg.Error("additional error encountered during AbortBlobUpload: " + err.Error())
			}
			_, err = a.db.Delete(upload)
			if err != nil {
				logg.Error("additional error encountered while deleting Upload from DB: " + err.Error())
			}
			return
		}
		chunkSizeBytes = &val
	}

	//append request body to upload
	digestState, err := a.streamIntoUpload(*account, upload, dw, r.Body, chunkSizeBytes)
	if respondWithError(w, err) {
		return
	}

	query := url.Values{}
	query.Set("state", digestState)
	w.Header().Set("Blob-Upload-Session-Id", upload.UUID)
	w.Header().Set("Content-Length", "0")
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/%s/blobs/uploads/%s?%s", account.Name, repoName, upload.UUID, query.Encode()))
	w.Header().Set("Range", fmt.Sprintf("0-%d", upload.SizeBytes))
	w.WriteHeader(http.StatusAccepted)
}

//This implements the PUT /v2/<account>/<repository>/blobs/uploads/<uuid> endpoint.
func (a *API) handleFinishBlobUpload(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/v2/:account/:repo/blobs/uploads/:uuid")
	account, repoName, _ := a.checkAccountAccess(w, r)
	if account == nil {
		return
	}
	upload := a.findUpload(w, r, *account, repoName)
	if upload == nil {
		return
	}
	query := r.URL.Query()
	dw, rerr := a.resumeUpload(*account, upload, query.Get("state"))
	if rerr != nil {
		rerr.WriteAsRegistryV2ResponseTo(w)
		return
	}

	//if we have a request body and Content-Length, append a final segment to the upload
	if contentLengthStr := r.Header.Get("Content-Length"); contentLengthStr != "" {
		contentLength, err := strconv.ParseUint(contentLengthStr, 10, 64)
		if err != nil {
			//COVERAGE: unreachable in unit tests because net/http validates Content-Length header format before sending
			keppel.ErrSizeInvalid.With("malformed Content-Length: " + err.Error())
			return
		}
		if contentLength > 0 {
			_, err = a.streamIntoUpload(*account, upload, dw, r.Body, &contentLength)
			if respondWithError(w, err) {
				return
			}
		}
	}

	//convert the Upload object into a Blob
	blob, err := a.finishUpload(*account, repoName, upload, query.Get("digest"))
	if respondWithError(w, err) {
		return
	}

	//count a finished blob push
	l := prometheus.Labels{"account": account.Name, "method": "registry-api"}
	api.BlobsPushedCounter.With(l).Inc()

	w.Header().Set("Content-Length", "0")
	w.Header().Set("Content-Range", fmt.Sprintf("0-%d", blob.SizeBytes))
	w.Header().Set("Docker-Content-Digest", blob.Digest)
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/%s/blobs/%s", account.Name, repoName, blob.Digest))
	w.WriteHeader(http.StatusCreated)
}

func (a *API) findUpload(w http.ResponseWriter, r *http.Request, account keppel.Account, repoName string) *keppel.Upload {
	uploadUUID := mux.Vars(r)["uuid"]

	upload, err := a.db.FindUploadByRepositoryName(uploadUUID, repoName, account)
	if err == sql.ErrNoRows {
		keppel.ErrBlobUploadUnknown.With("no such upload: " + uploadUUID).WriteAsRegistryV2ResponseTo(w)
		return nil
	}
	if respondWithError(w, err) {
		return nil
	}

	return upload
}

func (a *API) resumeUpload(account keppel.Account, upload *keppel.Upload, stateStr string) (dw *digestWriter, returnErr *keppel.RegistryV2Error) {
	//when encountering an error, cancel the upload entirely
	defer func() {
		if returnErr != nil {
			logg.Info("aborting upload because of error during resumeUpload()")
			countAbortedBlobUpload(account)
			err := a.sd.AbortBlobUpload(account, upload.StorageID, upload.NumChunks)
			if err != nil {
				logg.Error("additional error encountered during AbortBlobUpload: " + err.Error())
			}
			_, err = a.db.Delete(upload)
			if err != nil {
				logg.Error("additional error encountered while deleting Upload from DB: " + err.Error())
			}
		}
	}()

	//when an upload does not contain any data yet, stateStr should be empty
	//because there is nothing to resume
	if upload.NumChunks == 0 {
		if stateStr == "" {
			return &digestWriter{sha256.New(), 0}, nil
		}
		return nil, keppel.ErrBlobUploadInvalid.With("unexpected session state")
	}

	//when the upload *does* contain data, we have already sent that data through
	//SHA-256 and the corresponding hash.Hash instance should be in stateStr...
	stateBytes, err := base64.URLEncoding.DecodeString(stateStr)
	if err != nil {
		return nil, keppel.ErrBlobUploadInvalid.With("malformed session state")
	}
	hash := sha256.New()
	err = hash.(encoding.BinaryUnmarshaler).UnmarshalBinary(stateBytes)
	if err != nil {
		//COVERAGE: I've tried, but couldn't build a test where the session state
		//is corrupted specifically to go through the Base64 decoding, but not
		//through this step.
		return nil, keppel.ErrBlobUploadInvalid.With("broken session state")
	}

	//...and the digest from the data up until this point should be equal to upload.Digest
	stateDigest := digest.NewDigest(digest.SHA256, hash)
	if stateDigest.String() != upload.Digest {
		return nil, keppel.ErrBlobUploadInvalid.With("provided session state did not match uploaded content")
	}

	//we need to unmarshal the digest state once more because taking a Sum over
	//this hash may have altered the state
	hash = sha256.New()
	err = hash.(encoding.BinaryUnmarshaler).UnmarshalBinary(stateBytes)
	if err != nil {
		//COVERAGE: This branch is defense in depth. We unmarshaled the same state
		//above, so hitting an error just here should be impossible.
		return nil, keppel.ErrBlobUploadInvalid.With("broken session state")
	}

	return &digestWriter{hash, upload.SizeBytes}, nil
}

var contentRangeRx = regexp.MustCompile(`^([0-9]+)-([0-9]+)$`)

//On success, returns the number of bytes that should be in this request's body.
func (a *API) parseContentRange(upload *keppel.Upload, hdr http.Header) (uint64, error) {
	//some clients format Content-Range as `bytes=123-456` instead of just `123-456`
	contentRangeStr := strings.TrimPrefix(hdr.Get("Content-Range"), "bytes=")

	match := contentRangeRx.FindStringSubmatch(contentRangeStr)
	if match == nil {
		return 0, errors.New("malformed Content-Range")
	}
	rangeStart, err := strconv.ParseUint(match[1], 10, 64)
	if err != nil {
		return 0, errors.New("malformed Content-Range: " + err.Error())
	}
	rangeEnd, err := strconv.ParseUint(match[2], 10, 64)
	if err != nil {
		return 0, errors.New("malformed Content-Range: " + err.Error())
	}

	lengthStr := hdr.Get("Content-Length")
	if lengthStr == "" {
		return 0, errors.New("missing Content-Length for chunked upload")
	}
	length, err := strconv.ParseUint(lengthStr, 10, 64)
	if err != nil {
		//COVERAGE: unreachable in unit tests because net/http validates Content-Length header format before sending
		return 0, errors.New("malformed Content-Length: " + err.Error())
	}

	if rangeStart != upload.SizeBytes {
		return 0, fmt.Errorf("upload resumed at wrong offset: %d != %d", rangeStart, upload.SizeBytes)
	}
	if (rangeEnd - rangeStart) != length {
		return 0, fmt.Errorf("Content-Range contains %d bytes, but Content-Length is %d", rangeEnd-rangeStart, length)
	}
	return length, nil
}

func (a *API) streamIntoUpload(account keppel.Account, upload *keppel.Upload, dw *digestWriter, chunk io.Reader, chunkSizeBytes *uint64) (digestState string, returnErr error) {
	//if anything happens during this operation, we likely have produced an
	//inconsistent state between DB, storage backend and our internal book
	//keeping (esp. the digestState in dw.Hash), so we will have to abort the
	//upload entirely
	defer func() {
		if returnErr != nil {
			logg.Info("aborting upload because of error during streamIntoUpload()")
			countAbortedBlobUpload(account)
			err := a.sd.AbortBlobUpload(account, upload.StorageID, upload.NumChunks)
			if err != nil {
				logg.Error("additional error encountered during AbortBlobUpload: " + err.Error())
			}
			_, err = a.db.Delete(upload)
			if err != nil {
				logg.Error("additional error encountered while deleting Upload from DB: " + err.Error())
			}
		}
	}()

	//stream data from request body into storage
	upload.NumChunks++
	err := a.sd.AppendToBlob(account, upload.StorageID, upload.NumChunks, chunkSizeBytes, io.TeeReader(chunk, dw))
	if err != nil {
		return "", err
	}

	//if chunkSizeBytes is known, check that we wrote that many bytes
	actualChunkSizeBytes := dw.bytesWritten - upload.SizeBytes
	if chunkSizeBytes != nil && *chunkSizeBytes != actualChunkSizeBytes {
		msg := fmt.Sprintf("expected upload of %d bytes, but request contained only %d bytes",
			*chunkSizeBytes, actualChunkSizeBytes,
		)
		return "", keppel.ErrSizeInvalid.With(msg)
	}

	//serialize digest state for next resumeUpload() - note that we do this
	//BEFORE digest.NewDigest() because digest.NewDigest() may alter the
	//internal state of `dw.Hash`
	digestStateBytes, err := dw.Hash.(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		return "", err
	}

	//update Upload object in DB
	upload.SizeBytes = dw.bytesWritten
	upload.Digest = digest.NewDigest(digest.SHA256, dw.Hash).String()
	upload.UpdatedAt = a.timeNow()
	_, err = a.db.Update(upload)
	if err != nil {
		return "", err
	}

	return base64.URLEncoding.EncodeToString(digestStateBytes), nil
}

func (a *API) finishUpload(account keppel.Account, repoName string, upload *keppel.Upload, blobDigestStr string) (blob *keppel.Blob, returnErr error) {
	//if anything happens during this operation, we likely have produced an
	//inconsistent state between DB, storage backend and our internal book
	//keeping (esp. the digestState in dw.Hash), so we will have to abort the
	//upload entirely
	defer func() {
		if returnErr != nil {
			//TODO: might have to use DeleteBlob instead of AbortBlobUpload if the
			//error occurs after successful FinalizeBlob call
			logg.Info("aborting upload because of error during finishUpload()")
			countAbortedBlobUpload(account)
			err := a.sd.AbortBlobUpload(account, upload.StorageID, upload.NumChunks)
			if err != nil {
				logg.Error("additional error encountered during AbortBlobUpload: " + err.Error())
			}
			_, err = a.db.Delete(upload)
			if err != nil {
				logg.Error("additional error encountered while deleting Upload from DB: " + err.Error())
			}
		}
	}()

	//validate the digest provided by the user
	if blobDigestStr == "" {
		return nil, keppel.ErrDigestInvalid.With("missing digest")
	}
	blobDigest, err := digest.Parse(blobDigestStr)
	if err != nil {
		return nil, keppel.ErrDigestInvalid.With(err.Error())
	}
	if blobDigest.String() != upload.Digest {
		return nil, keppel.ErrDigestInvalid.With("")
	}

	//prepare database changes
	repo, err := a.db.FindOrCreateRepository(repoName, account)
	if err != nil {
		return nil, err
	}
	tx, err := a.db.Begin()
	if err != nil {
		return nil, err
	}
	defer keppel.RollbackUnlessCommitted(tx)

	_, err = tx.Delete(upload)
	if err != nil {
		return nil, err
	}

	blobPushedAt := a.timeNow()
	blob = &keppel.Blob{
		AccountName: account.Name,
		Digest:      blobDigest.String(),
		SizeBytes:   upload.SizeBytes,
		StorageID:   upload.StorageID,
		PushedAt:    blobPushedAt,
		ValidatedAt: blobPushedAt,
	}
	onCommit, err := a.createOrUpdateBlobObject(tx, blob, account)
	if err != nil {
		return nil, err
	}
	err = keppel.MountBlobIntoRepo(tx, *blob, *repo)
	if err != nil {
		return nil, err
	}

	//finally commit the blob to the storage backend and to the DB
	err = a.sd.FinalizeBlob(account, upload.StorageID, upload.NumChunks)
	if err != nil {
		return nil, err
	}
	err = tx.Commit()
	if err != nil {
		return nil, err
	}
	if onCommit != nil {
		onCommit()
	}
	return blob, nil
}

//Insert a Blob object in the database. This is similar to tx.Insert(blob), but
//handles a collision where another blob with the same account name and digest
//already exists in the database.
func (a *API) createOrUpdateBlobObject(tx *gorp.Transaction, blob *keppel.Blob, account keppel.Account) (onCommit func(), returnErr error) {
	//check for collision
	var otherBlob keppel.Blob
	err := tx.SelectOne(&otherBlob,
		`SELECT * FROM blobs WHERE account_name = $1 AND digest = $2`,
		blob.AccountName, blob.Digest)

	switch err {
	case sql.ErrNoRows:
		//no collision - just insert the new blob
		return nil, tx.Insert(blob)
	case nil:
		//collision - replace old blob with new blob (we trust the new blob more
		//because we just verified its digest)
		blob.ID = otherBlob.ID
		_, err := tx.Update(blob)
		onCommit := func() {
			//when the UPDATE was committed, we need to cleanup the old blob's contents
			err := a.sd.DeleteBlob(account, otherBlob.StorageID)
			if err != nil {
				logg.Error("additional error encountered while deleting duplicate blob %s from %s: %s", otherBlob.StorageID, account.Name, err.Error())
			}
		}
		return onCommit, err
	default:
		//unexpected error during SELECT
		return nil, err
	}
}

//digestWriter is an io.Writer that writes into the given Hash and also tracks the number of bytes written.
type digestWriter struct {
	hash.Hash
	bytesWritten uint64
}

func (w *digestWriter) Write(buf []byte) (n int, err error) {
	n, err = w.Hash.Write(buf)
	if n > 0 {
		w.bytesWritten += uint64(n)
	}
	return n, err
}

func countAbortedBlobUpload(account keppel.Account) {
	l := prometheus.Labels{"account": account.Name, "method": "registry-api"}
	api.UploadsAbortedCounter.With(l).Inc()
}
