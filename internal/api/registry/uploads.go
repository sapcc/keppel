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
	"context"
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
	"time"

	"github.com/go-gorp/gorp/v3"
	"github.com/gofrs/uuid"
	"github.com/gorilla/mux"
	"github.com/opencontainers/go-digest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/api"
	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// This implements the POST /v2/<account>/<repository>/blobs/uploads/ endpoint.
func (a *API) handleStartBlobUpload(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/v2/:account/:repo/blobs/uploads/")
	account, repo, authz := a.checkAccountAccess(w, r, createRepoIfMissing, nil)
	if account == nil {
		return
	}

	err := api.CheckRateLimit(r, a.rle, *account, authz, keppel.BlobPushAction, 1)
	if respondWithError(w, r, err) {
		return
	}

	//forbid pushing into replica accounts
	if account.UpstreamPeerHostName != "" {
		msg := fmt.Sprintf("cannot push into replica account (push to %s/%s instead!)",
			account.UpstreamPeerHostName, repo.FullName(),
		)
		keppel.ErrUnsupported.With(msg).WithStatus(http.StatusMethodNotAllowed).WriteAsRegistryV2ResponseTo(w, r)
		return
	}
	if account.ExternalPeerURL != "" {
		msg := fmt.Sprintf("cannot push into external replica account (push to %s/%s instead!)",
			account.ExternalPeerURL, repo.Name,
		)
		keppel.ErrUnsupported.With(msg).WithStatus(http.StatusMethodNotAllowed).WriteAsRegistryV2ResponseTo(w, r)
		return
	}

	//forbid pushing during maintenance
	if account.InMaintenance {
		keppel.ErrUnsupported.With("account is in maintenance").WithStatus(http.StatusMethodNotAllowed).WriteAsRegistryV2ResponseTo(w, r)
		return
	}

	//only allow new blob uploads when there is enough quota to push a manifest
	//
	//This is not strictly necessary to enforce the manifest quota, but it's
	//useful to avoid the accumulation of unreferenced blobs in the account's
	//backing storage.
	quotas, err := keppel.FindQuotas(a.db, account.AuthTenantID)
	if respondWithError(w, r, err) {
		return
	}
	if quotas == nil {
		quotas = keppel.DefaultQuotas(account.AuthTenantID)
	}
	manifestUsage, err := quotas.GetManifestUsage(a.db)
	if respondWithError(w, r, err) {
		return
	}
	if manifestUsage >= quotas.ManifestCount {
		msg := fmt.Sprintf("manifest quota exceeded (quota = %d, usage = %d)",
			quotas.ManifestCount, manifestUsage,
		)
		keppel.ErrDenied.With(msg).WithStatus(http.StatusConflict).WriteAsRegistryV2ResponseTo(w, r)
		return
	}

	//special case: request for cross-repo blob mount
	query := r.URL.Query()
	if sourceRepoFullName := query.Get("from"); sourceRepoFullName != "" {
		a.performCrossRepositoryBlobMount(w, r, *account, *repo, authz, sourceRepoFullName, query.Get("mount"))
		return
	}

	//special case: monolithic upload
	if blobDigestStr := query.Get("digest"); blobDigestStr != "" {
		a.performMonolithicUpload(w, r, *account, *repo, authz, blobDigestStr)
		return
	}

	//start a new upload
	uuidV4, err := uuid.NewV4()
	if respondWithError(w, r, err) {
		return
	}
	upload := keppel.Upload{
		RepositoryID: repo.ID,
		UUID:         uuidV4.String(),
		StorageID:    a.generateStorageID(),
		SizeBytes:    0,
		Digest:       "",
		NumChunks:    0,
		UpdatedAt:    a.timeNow(),
	}

	err = a.db.WithContext(r.Context()).Insert(&upload)
	if respondWithError(w, r, err) {
		return
	}

	w.Header().Set("Blob-Upload-Session-Id", upload.UUID)
	w.Header().Set("Content-Length", "0")
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s", getRepoNameForURLPath(*repo, authz), upload.UUID))
	w.Header().Set("Range", "0-0")
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) performCrossRepositoryBlobMount(w http.ResponseWriter, r *http.Request, account keppel.Account, targetRepo keppel.Repository, authz *auth.Authorization, sourceRepoFullName, blobDigestStr string) {
	//validate source repository
	if !strings.HasPrefix(sourceRepoFullName, account.Name+"/") {
		keppel.ErrUnsupported.With("cannot mount blobs across different accounts").WriteAsRegistryV2ResponseTo(w, r)
		return
	}
	sourceRepoName := strings.TrimPrefix(sourceRepoFullName, account.Name+"/")
	if !models.RepoNameWithLeadingSlashRx.MatchString("/" + sourceRepoName) {
		keppel.ErrNameInvalid.With("source repository is invalid").WriteAsRegistryV2ResponseTo(w, r)
		return
	}
	sourceRepo, err := keppel.FindRepository(a.db, sourceRepoName, account)
	if errors.Is(err, sql.ErrNoRows) {
		keppel.ErrNameUnknown.With("source repository does not exist").WriteAsRegistryV2ResponseTo(w, r)
		return
	}
	if respondWithError(w, r, err) {
		return
	}

	//validate blob
	blobDigest, err := digest.Parse(blobDigestStr)
	if err != nil {
		keppel.ErrDigestInvalid.With(err.Error()).WriteAsRegistryV2ResponseTo(w, r)
		return
	}
	blob, err := keppel.FindBlobByRepository(a.db, blobDigest, *sourceRepo)
	if errors.Is(err, sql.ErrNoRows) {
		keppel.ErrBlobUnknown.With("blob does not exist in source repository").WriteAsRegistryV2ResponseTo(w, r)
		return
	}
	if respondWithError(w, r, err) {
		return
	}

	//create blob mount if missing
	err = keppel.MountBlobIntoRepo(a.db, *blob, targetRepo)
	if respondWithError(w, r, err) {
		return
	}

	//the spec wants a Blob-Upload-Session-Id header even though the upload is done, so just make something up
	uuidV4, err := uuid.NewV4()
	if respondWithError(w, r, err) {
		return
	}
	w.Header().Set("Blob-Upload-Session-Id", uuidV4.String())
	w.Header().Set("Content-Length", "0")
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/%s", getRepoNameForURLPath(targetRepo, authz), blobDigest.String()))
	w.WriteHeader(http.StatusCreated)
}

func (a *API) performMonolithicUpload(w http.ResponseWriter, r *http.Request, account keppel.Account, repo keppel.Repository, authz *auth.Authorization, blobDigestStr string) (ok bool) {
	blobDigest, err := digest.Parse(blobDigestStr)
	if err != nil {
		keppel.ErrDigestInvalid.With(err.Error()).WriteAsRegistryV2ResponseTo(w, r)
		return false
	}

	//parse Content-Length
	sizeBytesStr := r.Header.Get("Content-Length")
	if sizeBytesStr == "" {
		keppel.ErrSizeInvalid.With("missing Content-Length header").WriteAsRegistryV2ResponseTo(w, r)
		return false
	}
	sizeBytes, err := strconv.ParseUint(sizeBytesStr, 10, 64)
	if sizeBytesStr == "" {
		//COVERAGE: unreachable in unit tests because net/http validates Content-Length header format before sending
		keppel.ErrSizeInvalid.With("invalid Content-Length: "+err.Error()).WriteAsRegistryV2ResponseTo(w, r)
		return false
	}

	//stream request body into the storage backend while also computing the digest and length
	upload := keppel.Upload{
		StorageID: a.generateStorageID(),
		SizeBytes: 0,
		NumChunks: 0,
	}
	dw := digestWriter{Hash: sha256.New()}
	err = a.processor().AppendToBlob(account, &upload, io.TeeReader(r.Body, &dw), &sizeBytes)
	if err == nil {
		err = a.sd.FinalizeBlob(account, upload.StorageID, upload.NumChunks)
	}
	if respondWithError(w, r, err) {
		countAbortedBlobUpload(account)
		err := a.sd.AbortBlobUpload(account, upload.StorageID, upload.NumChunks)
		if err != nil {
			logg.Error("additional error encountered while aborting blob upload %s into %s: %s", upload.StorageID, repo.FullName(), err.Error())
		}
		return false
	}

	//if any of the remaining steps fail, don't forget to cleanup the storage backend
	defer func() {
		if !ok {
			countAbortedBlobUpload(account)
			err := a.sd.DeleteBlob(account, upload.StorageID)
			if err != nil {
				logg.Error("additional error encountered while deleting broken blob %s from %s: %s", upload.StorageID, repo.FullName(), err.Error())
			}
			return
		}
	}()

	//validate digest and length
	if dw.bytesWritten != sizeBytes {
		keppel.ErrSizeInvalid.With("Content-Length was %d, but %d bytes were sent", sizeBytes, dw.bytesWritten).WriteAsRegistryV2ResponseTo(w, r)
		return false
	}

	actualDigest := digest.NewDigest(digest.SHA256, dw.Hash)
	if actualDigest != blobDigest {
		keppel.ErrDigestInvalid.With("expected %s, but actual digest was %s", blobDigest.String(), actualDigest.String()).WriteAsRegistryV2ResponseTo(w, r)
		return false
	}

	//record blob in DB
	tx, err := a.db.Begin()
	if respondWithError(w, r, err) {
		return false
	}
	defer sqlext.RollbackUnlessCommitted(tx)

	blobPushedAt := a.timeNow()
	blob, err := a.createOrUpdateBlobObject(tx, sizeBytes, upload.StorageID, blobDigest, blobPushedAt, account)
	if respondWithError(w, r, err) {
		return false
	}
	err = keppel.MountBlobIntoRepo(tx, *blob, repo)
	if respondWithError(w, r, err) {
		return false
	}
	err = tx.Commit()
	if respondWithError(w, r, err) {
		return false
	}

	//the spec wants a Blob-Upload-Session-Id header even though the upload is done, so just make something up
	uuidV4, err := uuid.NewV4()
	if respondWithError(w, r, err) {
		return
	}
	w.Header().Set("Blob-Upload-Session-Id", uuidV4.String())
	w.Header().Set("Content-Length", "0")
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/%s", getRepoNameForURLPath(repo, authz), blobDigest.String()))
	w.WriteHeader(http.StatusCreated)
	return true
}

// This implements the DELETE /v2/<account>/<repository>/blobs/uploads/<uuid> endpoint.
func (a *API) handleDeleteBlobUpload(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/v2/:account/:repo/blobs/uploads/:uuid")
	account, repo, _ := a.checkAccountAccess(w, r, failIfRepoMissing, nil)
	if account == nil {
		return
	}
	upload := a.findUpload(w, r, *repo)
	if upload == nil {
		return
	}

	//prepare the database transaction for deleting this upload
	tx, err := a.db.Begin()
	if respondWithError(w, r, err) {
		return
	}
	defer sqlext.RollbackUnlessCommitted(tx)
	_, err = tx.Delete(upload)
	if respondWithError(w, r, err) {
		return
	}

	//perform the deletion in the storage backend, then make the DB change durable
	if upload.NumChunks > 0 {
		err = a.sd.AbortBlobUpload(*account, upload.StorageID, upload.NumChunks)
		if respondWithError(w, r, err) {
			return
		}
	}
	err = tx.Commit()
	if respondWithError(w, r, err) {
		return
	}

	//report success
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusNoContent)
}

// This implements the GET /v2/<account>/<repository>/blobs/uploads/<uuid> endpoint.
func (a *API) handleGetBlobUpload(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/v2/:account/:repo/blobs/uploads/:uuid")

	account, repo, authz := a.checkAccountAccess(w, r, failIfRepoMissing, nil)
	if account == nil {
		return
	}
	upload := a.findUpload(w, r, *repo)
	if upload == nil {
		return
	}

	w.Header().Set("Blob-Upload-Session-Id", upload.UUID)
	w.Header().Set("Content-Length", "0")
	w.Header().Set("Range", makeRangeHeader(upload.SizeBytes))

	//if the request URL is from the "Location" header of a previous upload chunk,
	//the OCI Distribution Spec as of v1.1.0 requires us to display the upload
	//URL in the "Location" header
	if upload.SizeBytes == 0 {
		//case 1: if the upload did not have any data sent into it, we can build
		//the upload URL from our DB alone
		w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s",
			getRepoNameForURLPath(*repo, authz), upload.UUID,
		))
	} else if stateStr := r.URL.Query().Get("state"); stateStr != "" {
		//case 2: if the upload had data sent into it, we need the hash state
		//that's included in the Location URL
		w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s?%s",
			getRepoNameForURLPath(*repo, authz), upload.UUID, url.Values{"state": {stateStr}}.Encode(),
		))
	}

	w.WriteHeader(http.StatusNoContent)
}

// This implements the PATCH /v2/<account>/<repository>/blobs/uploads/<uuid> endpoint.
func (a *API) handleContinueBlobUpload(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/v2/:account/:repo/blobs/uploads/:uuid")
	account, repo, authz := a.checkAccountAccess(w, r, failIfRepoMissing, nil)
	if account == nil {
		return
	}
	upload := a.findUpload(w, r, *repo)
	if upload == nil {
		return
	}
	dw, rerr := a.resumeUpload(r.Context(), *account, upload, r.URL.Query().Get("state"))
	if rerr != nil {
		rerr.WriteAsRegistryV2ResponseTo(w, r)
		return
	}

	//if we have the Content-Range and Content-Length headers ("chunked upload mode"),
	//parse and validate them
	chunkSizeBytes := (*uint64)(nil)
	if r.Header.Get("Content-Range") != "" {
		lengthBytes, err := a.parseContentRange(upload, r.Header)
		if err != nil {
			keppel.ErrSizeInvalid.With(err.Error()).WithStatus(http.StatusRequestedRangeNotSatisfiable).WriteAsRegistryV2ResponseTo(w, r)

			logg.Info("aborting upload because of error during parseContentRange()")
			countAbortedBlobUpload(*account)
			err := a.sd.AbortBlobUpload(*account, upload.StorageID, upload.NumChunks)
			if err != nil {
				logg.Error("additional error encountered during AbortBlobUpload: " + err.Error())
			}
			_, err = a.db.WithContext(r.Context()).Delete(upload)
			if err != nil {
				logg.Error("additional error encountered while deleting Upload from DB: " + err.Error())
			}
			return
		}
		chunkSizeBytes = &lengthBytes
	}

	//append request body to upload
	digestState, err := a.streamIntoUpload(r.Context(), *account, upload, dw, r.Body, chunkSizeBytes)
	if respondWithError(w, r, err) {
		return
	}

	query := url.Values{}
	query.Set("state", digestState)
	w.Header().Set("Blob-Upload-Session-Id", upload.UUID)
	w.Header().Set("Content-Length", "0")
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s?%s", getRepoNameForURLPath(*repo, authz), upload.UUID, query.Encode()))
	w.Header().Set("Range", makeRangeHeader(upload.SizeBytes))
	w.WriteHeader(http.StatusAccepted)
}

// This implements the PUT /v2/<account>/<repository>/blobs/uploads/<uuid> endpoint.
func (a *API) handleFinishBlobUpload(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/v2/:account/:repo/blobs/uploads/:uuid")
	account, repo, authz := a.checkAccountAccess(w, r, failIfRepoMissing, nil)
	if account == nil {
		return
	}
	upload := a.findUpload(w, r, *repo)
	if upload == nil {
		return
	}
	query := r.URL.Query()
	dw, rerr := a.resumeUpload(r.Context(), *account, upload, query.Get("state"))
	if rerr != nil {
		rerr.WriteAsRegistryV2ResponseTo(w, r)
		return
	}

	//if we have a request body and Content-Length, append a final segment to the upload
	if contentLengthStr := r.Header.Get("Content-Length"); contentLengthStr != "" {
		contentLength, err := strconv.ParseUint(contentLengthStr, 10, 64)
		if err != nil {
			//COVERAGE: unreachable in unit tests because net/http validates Content-Length header format before sending
			keppel.ErrSizeInvalid.With("malformed Content-Length: "+err.Error()).WriteAsRegistryV2ResponseTo(w, r)
			return
		}
		if contentLength > 0 {
			_, err = a.streamIntoUpload(r.Context(), *account, upload, dw, r.Body, &contentLength)
			if respondWithError(w, r, err) {
				return
			}
		}
	}

	//convert the Upload into a Blob in both the storage backend and the DB
	//
	//NOTE 1: This is written a bit funny to avoid duplicating error handling
	//code for each step.
	//NOTE 2: Since we finalize the blob in the storage first, there's a slight
	//chance that unexpected errors could leave us with a dangling blob in the
	//storage that the DB does not know about, but the storage sweep can clean
	//that up later.
	var blob *keppel.Blob
	err := a.sd.FinalizeBlob(*account, upload.StorageID, upload.NumChunks)
	if err == nil {
		blob, err = a.createBlobFromUpload(*account, *repo, *upload, query.Get("digest"))
	}

	//if an error occurred anywhere during this last sequence of steps, do our best to clean up the mess we left behind
	if respondWithError(w, r, err) {
		countAbortedBlobUpload(*account)
		_, err := a.db.WithContext(r.Context()).Delete(upload)
		if err != nil {
			logg.Error("additional error encountered while deleting Upload from DB after late upload error: " + err.Error())
		}
		err = a.sd.DeleteBlob(*account, upload.StorageID)
		if err != nil {
			logg.Error("additional error encountered during DeleteBlob() after late upload error: " + err.Error())
		}
		return
	}

	//count a finished blob push
	l := prometheus.Labels{"account": account.Name, "auth_tenant_id": account.AuthTenantID, "method": "registry-api"}
	api.BlobsPushedCounter.With(l).Inc()
	api.BlobBytesPushedCounter.With(l).Add(float64(blob.SizeBytes))

	w.Header().Set("Content-Length", "0")
	w.Header().Set("Content-Range", makeRangeHeader(blob.SizeBytes))
	w.Header().Set("Docker-Content-Digest", blob.Digest.String())
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/%s", getRepoNameForURLPath(*repo, authz), blob.Digest))
	w.WriteHeader(http.StatusCreated)
}

func (a *API) findUpload(w http.ResponseWriter, r *http.Request, repo keppel.Repository) *keppel.Upload {
	uploadUUID := mux.Vars(r)["uuid"]

	upload, err := keppel.FindUploadByRepository(a.db, uploadUUID, repo)
	if errors.Is(err, sql.ErrNoRows) {
		keppel.ErrBlobUploadUnknown.With("no such upload: "+uploadUUID).WriteAsRegistryV2ResponseTo(w, r)
		return nil
	}
	if respondWithError(w, r, err) {
		return nil
	}

	return upload
}

func (a *API) resumeUpload(ctx context.Context, account keppel.Account, upload *keppel.Upload, stateStr string) (dw *digestWriter, returnErr *keppel.RegistryV2Error) {
	//when encountering an error, cancel the upload entirely
	defer func() {
		if returnErr != nil {
			logg.Info("aborting upload because of error during resumeUpload()")
			countAbortedBlobUpload(account)
			err := a.sd.AbortBlobUpload(account, upload.StorageID, upload.NumChunks)
			if err != nil {
				logg.Error("additional error encountered during AbortBlobUpload: " + err.Error())
			}
			_, err = a.db.WithContext(ctx).Delete(upload)
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
	hashWriter := sha256.New()
	err = hashWriter.(encoding.BinaryUnmarshaler).UnmarshalBinary(stateBytes)
	if err != nil {
		return nil, keppel.ErrBlobUploadInvalid.With("broken session state").WithStatus(http.StatusRequestedRangeNotSatisfiable)
	}

	//...and the digest from the data up until this point should be equal to upload.Digest
	stateDigest := digest.NewDigest(digest.SHA256, hashWriter)
	if stateDigest.String() != upload.Digest {
		return nil, keppel.ErrBlobUploadInvalid.With("provided session state did not match uploaded content")
	}

	//we need to unmarshal the digest state once more because taking a Sum over
	//this hash may have altered the state
	hashWriter = sha256.New()
	err = hashWriter.(encoding.BinaryUnmarshaler).UnmarshalBinary(stateBytes)
	if err != nil {
		//COVERAGE: This branch is defense in depth. We unmarshaled the same state
		//above, so hitting an error just here should be impossible.
		return nil, keppel.ErrBlobUploadInvalid.With("broken session state").WithStatus(http.StatusRequestedRangeNotSatisfiable)
	}

	return &digestWriter{hashWriter, upload.SizeBytes}, nil
}

var contentRangeRx = regexp.MustCompile(`^([0-9]+)-([0-9]+)$`)

// On success, returns the number of bytes that should be in this request's body.
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
	if (rangeEnd + 1 - rangeStart) != length {
		return 0, fmt.Errorf("Content-Range contains %d bytes, but Content-Length is %d", rangeEnd+1-rangeStart, length)
	}
	return length, nil
}

func (a *API) streamIntoUpload(ctx context.Context, account keppel.Account, upload *keppel.Upload, dw *digestWriter, chunk io.Reader, chunkSizeBytes *uint64) (digestState string, returnErr error) {
	db := a.db.WithContext(ctx)

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
			_, err = db.Delete(upload)
			if err != nil {
				logg.Error("additional error encountered while deleting Upload from DB: " + err.Error())
			}
		}
	}()

	//stream data from request body into storage
	sizeBytesBefore := upload.SizeBytes
	err := a.processor().AppendToBlob(account, upload, io.TeeReader(chunk, dw), chunkSizeBytes)
	if err != nil {
		return "", err
	}

	//if chunkSizeBytes is known, check that we wrote that many bytes, not more, not less
	actualChunkSizeBytes := dw.bytesWritten - sizeBytesBefore
	if chunkSizeBytes != nil && *chunkSizeBytes != actualChunkSizeBytes {
		msg := fmt.Sprintf("expected upload of %d bytes, but request contained %d bytes",
			*chunkSizeBytes, actualChunkSizeBytes,
		)
		return "", keppel.ErrSizeInvalid.With(msg).WithStatus(http.StatusRequestedRangeNotSatisfiable)
	}

	//serialize digest state for next resumeUpload() - note that we do this
	//BEFORE digest.NewDigest() because digest.NewDigest() may alter the
	//internal state of `dw.Hash`
	digestStateBytes, err := dw.Hash.(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		return "", err
	}

	//update Upload object in DB
	upload.Digest = digest.NewDigest(digest.SHA256, dw.Hash).String()
	upload.UpdatedAt = a.timeNow()
	_, err = db.Update(upload)
	if err != nil {
		return "", err
	}

	return base64.URLEncoding.EncodeToString(digestStateBytes), nil
}

func (a *API) createBlobFromUpload(account keppel.Account, repo keppel.Repository, upload keppel.Upload, blobDigestStr string) (blob *keppel.Blob, returnErr error) {
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
	tx, err := a.db.Begin()
	if err != nil {
		return nil, err
	}
	defer sqlext.RollbackUnlessCommitted(tx)

	_, err = tx.Delete(&upload)
	if err != nil {
		return nil, err
	}

	blobPushedAt := a.timeNow()
	blob, err = a.createOrUpdateBlobObject(tx, upload.SizeBytes, upload.StorageID, blobDigest, blobPushedAt, account)
	if err != nil {
		return nil, err
	}
	err = keppel.MountBlobIntoRepo(tx, *blob, repo)
	if err != nil {
		return nil, err
	}
	return blob, tx.Commit()
}

var insertBlobIfMissingQuery = sqlext.SimplifyWhitespace(`
	INSERT INTO blobs (account_name, digest, size_bytes, storage_id, pushed_at, validated_at)
	VALUES ($1, $2, $3, $4, $5, $5)
	ON CONFLICT DO NOTHING
`)

// Insert a Blob object in the database. This is similar to building a
// keppel.Blob and doing tx.Insert(blob), but handles a collision where another
// blob with the same account name and digest already exists in the database.
func (a *API) createOrUpdateBlobObject(tx *gorp.Transaction, sizeBytes uint64, storageID string, blobDigest digest.Digest, blobPushedAt time.Time, account keppel.Account) (*keppel.Blob, error) {
	//try to insert the blob atomically (I would like to SELECT the result
	//directly via `RETURNING *`, but that gives sql.ErrNoRows when nothing was
	//inserted because of ON CONFLICT, so in the general case, we need another
	//SELECT to get the resulting blob anyway)
	_, err := tx.Exec(insertBlobIfMissingQuery,
		account.Name, blobDigest.String(), sizeBytes, storageID, blobPushedAt,
	)
	if err != nil {
		return nil, err
	}
	blob, err := keppel.FindBlobByAccountName(tx, blobDigest, account)
	if err != nil {
		return nil, err
	}

	//if we already had a blob with this digest, there was a CONFLICT and we
	//obtained the existing blob from the SELECT; since we already have the
	//existing blob, we can discard the uploaded blob contents and reuse the
	//existing blob instead
	if blob.StorageID != storageID {
		err := a.sd.DeleteBlob(account, storageID)
		if err != nil {
			return nil, fmt.Errorf("while deleting duplicate blob contents for %s at storage ID %s: %w",
				blobDigest, storageID, err)
		}
	}

	return blob, nil
}

// digestWriter is an io.Writer that writes into the given Hash and also tracks the number of bytes written.
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
	l := prometheus.Labels{"account": account.Name, "auth_tenant_id": account.AuthTenantID, "method": "registry-api"}
	api.UploadsAbortedCounter.With(l).Inc()
}

func makeRangeHeader(sizeBytes uint64) string {
	if sizeBytes == 0 {
		return "0-0"
	}
	return fmt.Sprintf("0-%d", sizeBytes-1)
}
