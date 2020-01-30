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
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/docker/distribution"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
)

//Manifest describes a manifest that can be replicated into our local registry.
type Manifest struct {
	Account   keppel.Account
	Repo      keppel.Repository
	Reference string
}

//ReplicateManifest replicates the manifest from its account's upstream registry.
//
//If the replication was started successfully (or if it is already in progress
//elsewhere), the manifest's contents will be returned in an artificial
//keppel.PendingManifest instance. (Warning: This instance is not backed by a DB
//record and shall not be written into the DB.)
//
//When this function returns, the manifest's blobs may not be fully replicated
//yet. Blob replication will continue in a background goroutine. The
//context.Context argument can be used to abort blob replication after this
//call has returned.
func (r Replicator) ReplicateManifest(ctx context.Context, m Manifest) (keppel.PendingManifest, error) {
	//get a token for upstream
	var peer keppel.Peer
	err := r.db.SelectOne(&peer, `SELECT * FROM peers WHERE hostname = $1`, m.Account.UpstreamPeerHostName)
	if err != nil {
		return keppel.PendingManifest{}, err
	}
	peerToken, err := r.getPeerToken(peer, m.Repo.FullName())
	if err != nil {
		return keppel.PendingManifest{}, err
	}

	//query upstream for the manifest
	manifestReader, _, manifestContentType, err := r.fetchFromUpstream(m.Repo, "GET", "manifests/"+m.Reference, peer, peerToken)
	if err != nil {
		return keppel.PendingManifest{}, err
	}
	manifestBytes, err := ioutil.ReadAll(manifestReader)
	if err != nil {
		manifestReader.Close()
		return keppel.PendingManifest{}, err
	}
	err = manifestReader.Close()
	if err != nil {
		return keppel.PendingManifest{}, err
	}

	//validate manifest
	manifest, manifestDesc, err := distribution.UnmarshalManifest(manifestContentType, manifestBytes)
	if err != nil {
		return keppel.PendingManifest{}, keppel.ErrManifestInvalid.With(err.Error())
	}
	pm := keppel.PendingManifest{
		RepositoryID: m.Repo.ID,
		Reference:    m.Reference,
		Digest:       manifestDesc.Digest.String(),
		Reason:       keppel.PendingBecauseOfReplication,
		PendingSince: time.Now(),
		MediaType:    manifestDesc.MediaType,
		Content:      string(manifestBytes),
	}

	//fork background goroutine and only return to the caller once the background
	//goroutine is up and running
	//
	//This is to ensure the semantics of the pending_manifests table. For each
	//record in there, we want a running replication task somewhere. This is
	//ensured by a deferred deletion in replicateManifestAsync(). We only return
	//from this method when the deferred function is in effect.
	signal := make(chan error)
	go r.replicateManifestAsync(ctx, m, pm, manifest, signal)
	return pm, <-signal
}

func (r Replicator) replicateManifestAsync(ctx context.Context, m Manifest, pm keppel.PendingManifest, manifest distribution.Manifest, signal chan<- error) {
	//for replicating, we need a push-access token for our local keppel-registry
	localToken, err := auth.Token{
		UserName: "replication@" + r.cfg.APIPublicHostname(),
		Audience: r.cfg.APIPublicHostname(),
		Access: []auth.Scope{{
			ResourceType: "repository",
			ResourceName: m.Repo.FullName(),
			Actions:      []string{"pull", "push"},
		}},
	}.Issue(r.cfg)
	if err != nil {
		signal <- err
		return
	}

	//mark this manifest as currently being replicated
	err = r.db.Insert(&pm)
	if err != nil {
		//did we get a duplicate-key error because this manifest is already being replicated?
		count, err := r.db.SelectInt(
			`SELECT COUNT(*) FROM pending_manifests WHERE repo_id = $1 AND reference = $2`,
			pm.RepositoryID, pm.Reference,
		)
		if err == nil && count > 0 {
			//We do not signal `ErrConcurrentReplication` here. We have the manifest
			//to give to the client, and blobs are already being replicated
			//elsewhere. We can just report success.
			signal <- nil
			return
		}
		signal <- err
		return
	}

	//whatever happens, don't forget to cleanup the PendingManifest DB entry
	//afterwards (on success, we want it cleaned up to keep the DB clean and
	//avoid overlong-replication alerts; on error, we want to enable other
	//workers to restart the replication)
	defer func() {
		_, err := r.db.Delete(&pm)
		if err != nil {
			logg.Error("while trying to replicate manifest %s in %s/%s: %s",
				m.Reference, m.Account.Name, m.Repo.Name, err.Error())
		}
	}()
	//it is now safe for the current GET request to proceed
	signal <- nil

	//When pulling a Docker image, the Docker client pulls the manifest first,
	//then the image configuration, then the blobs. Errors that occur when
	//pulling a blob are considered recoverable and will cause the Docker client
	//to enter a retry loop. But pull errors for the manifest and image
	//configuration are not recoverable and make `docker pull` fail immediately.
	//At this point, the client is receiving the manifest from us and they want
	//to pull the image configuration next. If we started replicating blobs
	//immediately, there's a decent chance that we would start replicating the
	//image configuration blob, which would cause the client to see a 429 error.
	//That's bad UX, so we wait a bit to give the client a head start in pulling
	//the image configuration.
	time.Sleep(3 * time.Second)

	//replicate all blobs referenced by this manifest
	hadErrors := false
	remainingBlobs := manifest.References()
	for len(remainingBlobs) > 0 {
		//abort early if requested (esp. to allow graceful process termination to proceed)
		if ctx.Err() != nil {
			return
		}

		desc := remainingBlobs[0]
		err := r.replicateBlobIfNecessary(Blob{
			Account: m.Account,
			Repo:    m.Repo,
			Digest:  desc.Digest.String(),
		}, m, localToken.SignedToken)

		switch err {
		case nil:
			//blob was replicated - remove from worklist
			remainingBlobs = remainingBlobs[1:]
		case ErrConcurrentReplication:
			//someone else is replicating this blob - put it on the back of the
			//worklist and come back to it later
			remainingBlobs = append(remainingBlobs[1:], remainingBlobs[0])
			//also wait a bit before trying the next one to avoid excessive polling
			//on the DB when all remaining blobs are being replicated elsewhere
			time.Sleep(1 * time.Second)
		default:
			logg.Error("while trying to replicate blob %s in manifest %s in %s/%s: %s",
				desc.Digest.String(), m.Reference, m.Account.Name, m.Repo.Name, err.Error())
			hadErrors = true
			//skip the problematic blob
			remainingBlobs = remainingBlobs[1:]
		}
	}

	//we can only replicate the manifest if all blobs were replicated successfully
	if hadErrors {
		return
	}
	//abort early if requested (esp. to allow graceful process termination to proceed)
	if ctx.Err() != nil {
		return
	}

	//after all blobs have been replicated, we can push the manifest into our
	//local keppel-registry
	//
	//We push the manifest through our own API to re-use the logic in
	//registryv2.API.handlePutManifest().
	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s",
		r.cfg.APIPublicHostname(), m.Repo.FullName(), m.Reference,
	)
	req, err := http.NewRequest("PUT", url, strings.NewReader(pm.Content))
	if err != nil {
		m.logReplicationError(err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+localToken.SignedToken)
	req.Header.Set("Content-Type", pm.MediaType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		m.logReplicationError(err)
		return
	}
	if resp.StatusCode != http.StatusCreated {
		m.logReplicationError(unexpectedStatusCodeError{req, http.StatusCreated, resp.Status})
		return
	}
}

func (r Replicator) replicateBlobIfNecessary(b Blob, m Manifest, localToken string) error {
	//check if this blob needs to be replicated
	req, err := http.NewRequest("HEAD", fmt.Sprintf("/v2/%s/blobs/%s", b.Repo.FullName(), b.Digest), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+localToken)
	resp, err := r.od.DoHTTPRequest(b.Account, req, keppel.FollowRedirects)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusOK {
		//blob exists -> nothing to do
		return nil
	}

	logg.Info("replicating blob %s referenced by manifest %s in %s/%s",
		b.Digest, m.Reference, m.Account.Name, m.Repo.Name)
	_, err = r.ReplicateBlob(b, nil, "GET")
	return err
}

func (m Manifest) logReplicationError(err error) {
	logg.Error("while trying to replicate manifest %s in %s/%s: %s",
		m.Reference, m.Account.Name, m.Repo.Name, err.Error())
}
