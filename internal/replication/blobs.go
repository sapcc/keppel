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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sapcc/keppel/internal/auth"
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
	Digest  string
}

//ReplicateBlob replicates the blob with the given digest into the given
//repository from the b.Account's upstream registry.
//
//If a ResponseWriter is given, the response to the GET request to the upstream
//registry is also copied into it as the blob contents are being streamed into
//our local registry. The result value `responseWasWritten` indicates whether
//this happened. It may be false if an error occured before writing into the
//ResponseWriter took place.
func (r Replicator) ReplicateBlob(b Blob, w http.ResponseWriter) (responseWasWritten bool, returnErr error) {
	//mark this blob as currently being replicated
	pendingBlob := keppel.PendingBlob{
		RepositoryID: b.Repo.ID,
		Digest:       b.Digest,
		Reason:       keppel.PendingBecauseOfReplication,
		PendingSince: time.Now(),
	}
	err := r.db.Insert(&pendingBlob)
	if err != nil {
		//did we get a duplicate-key error because this blob is already being replicated?
		count, err := r.db.SelectInt(
			`SELECT * FROM pending_blobs WHERE repo_id = $1 AND digest = $2`,
			b.Repo.ID, b.Digest,
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
			b.Repo.ID, b.Digest,
		)
		if returnErr == nil {
			returnErr = err
		}
	}()

	//get a token for the local keppel-registry
	localToken, err := auth.Token{
		UserName: "replication@" + r.cfg.APIPublicHostname(),
		Audience: r.cfg.APIPublicHostname(),
		Access: []auth.Scope{{
			ResourceType: "repository",
			ResourceName: b.Repo.FullName(),
			Actions:      []string{"pull", "push"},
		}},
	}.Issue(r.cfg)
	if err != nil {
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
	blobReadCloser, blobLengthBytes, err := r.fetchFromUpstream(b, peer, peerToken)
	if err != nil {
		return false, err
	}
	defer blobReadCloser.Close()

	//stream into `w` if requested
	blobReader := io.Reader(blobReadCloser)
	if w != nil {
		w.Header().Set("Docker-Content-Digest", b.Digest)
		w.Header().Set("Content-Length", strconv.FormatUint(blobLengthBytes, 10))
		w.WriteHeader(http.StatusOK)
		blobReader = io.TeeReader(blobReader, w)
	}

	//upload into local keppel-registry
	return true, r.uploadToLocal(b, blobReader, blobLengthBytes, localToken.SignedToken)
}

func (r Replicator) fetchFromUpstream(b Blob, peer keppel.Peer, peerToken string) (body io.ReadCloser, bodyLengthBytes uint64, returnErr error) {
	reqURL := fmt.Sprintf(
		"https://%s/v2/%s/blobs/%s",
		peer.HostName, b.Repo.FullName(), b.Digest)

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+peerToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		//close resp.Body only if we're not passing it to the caller
		if body == nil {
			resp.Body.Close()
		}
	}()

	//on success, just return the response body
	if resp.StatusCode == http.StatusOK {
		blobLengthBytes, err := strconv.ParseUint(resp.Header.Get("Content-Length"), 10, 64)
		return resp.Body, blobLengthBytes, err
	}

	//on error, try to parse the upstream RegistryV2Error so that we can proxy it
	//through to the client correctly
	//
	//NOTE: We use HasPrefix here because the actual Content-Type is usually
	//"application/json; charset=utf-8".
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		var respData struct {
			Errors []*keppel.RegistryV2Error `json:"errors"`
		}
		err := json.NewDecoder(resp.Body).Decode(&respData)
		if err == nil && len(respData.Errors) > 0 {
			return nil, 0, respData.Errors[0].WithStatus(resp.StatusCode)
		}
	}
	return nil, 0, unexpectedStatusCodeError{req, http.StatusOK, resp.Status}
}

func (r Replicator) uploadToLocal(b Blob, blobReader io.Reader, blobLengthBytes uint64, localToken string) error {
	//start blob upload
	url1 := fmt.Sprintf("/v2/%s/blobs/uploads/", b.Repo.FullName())
	req1, err := http.NewRequest("POST", url1, nil)
	if err != nil {
		return err
	}
	req1.Header.Set("Authorization", "Bearer "+localToken)
	resp1, err := r.od.DoHTTPRequest(b.Account, req1, keppel.FollowRedirects)
	if err != nil {
		return err
	}
	if resp1.StatusCode != http.StatusAccepted {
		return unexpectedStatusCodeError{req1, http.StatusAccepted, resp1.Status}
	}

	//send blob data
	url2 := keppel.AppendQuery(resp1.Header.Get("Location"),
		url.Values{"digest": {b.Digest}},
	)
	req2, err := http.NewRequest("PUT", url2, blobReader)
	if err != nil {
		return err
	}
	req2.Header.Set("Authorization", "Bearer "+localToken)
	req2.Header.Set("Content-Type", "application/octet-stream")
	req2.Header.Set("Content-Length", strconv.FormatUint(blobLengthBytes, 10))
	resp2, err := r.od.DoHTTPRequest(b.Account, req2, keppel.FollowRedirects)
	if err != nil {
		return err
	}
	if resp2.StatusCode != http.StatusCreated {
		return unexpectedStatusCodeError{req2, http.StatusCreated, resp2.Status}
	}

	return nil
}
