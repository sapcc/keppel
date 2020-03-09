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
	"io/ioutil"

	"github.com/docker/distribution"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/processor"
)

//Manifest describes a manifest that can be replicated into our local registry.
type Manifest struct {
	Account   keppel.Account
	Repo      keppel.Repository
	Reference keppel.ManifestReference
}

//ReplicateManifest replicates the manifest from its account's upstream registry.
//On success, the manifest's metadata and contents are returned.
func (r Replicator) ReplicateManifest(m Manifest) (*keppel.Manifest, []byte, error) {
	//get a token for upstream
	var peer keppel.Peer
	err := r.db.SelectOne(&peer, `SELECT * FROM peers WHERE hostname = $1`, m.Account.UpstreamPeerHostName)
	if err != nil {
		return nil, nil, err
	}
	peerToken, err := r.getPeerToken(peer, m.Repo.FullName())
	if err != nil {
		return nil, nil, err
	}

	return r.replicateManifestRecursively(m, peer, peerToken)
}

func (r Replicator) replicateManifestRecursively(m Manifest, peer keppel.Peer, peerToken string) (*keppel.Manifest, []byte, error) {
	proc := processor.New(r.db, r.sd)

	//query upstream for the manifest
	manifestReader, _, manifestMediaType, err := r.fetchFromUpstream(m.Repo, "GET", "manifests/"+m.Reference.String(), peer, peerToken)
	if err != nil {
		return nil, nil, err
	}
	manifestBytes, err := ioutil.ReadAll(manifestReader)
	if err != nil {
		manifestReader.Close()
		return nil, nil, err
	}
	err = manifestReader.Close()
	if err != nil {
		return nil, nil, err
	}

	//parse the manifest to discover references to other manifests and blobs
	manifestParsed, _, err := distribution.UnmarshalManifest(manifestMediaType, manifestBytes)
	if err != nil {
		return nil, nil, keppel.ErrManifestInvalid.With(err.Error())
	}

	//mark all missing blobs as pending replication
	for _, desc := range manifestParsed.References() {
		if keppel.IsManifestMediaType(desc.MediaType) {
			//replicate referenced manifests recursively if required
			_, err := keppel.FindManifest(r.db, m.Repo, desc.Digest.String())
			if err == sql.ErrNoRows {
				_, _, err = r.replicateManifestRecursively(Manifest{
					Account:   m.Account,
					Repo:      m.Repo,
					Reference: keppel.ManifestReference{Digest: desc.Digest},
				}, peer, peerToken)
			}
			if err != nil {
				return nil, nil, err
			}
		} else {
			//mark referenced blobs as pending replication if not replicated yet
			blob, err := proc.FindBlobOrInsertUnbackedBlob(desc, m.Account)
			if err != nil {
				return nil, nil, err
			}
			//also ensure that the blob is mounted in this repo (this is also
			//important if the blob exists; it may only have been replicated in a
			//different repo)
			err = keppel.MountBlobIntoRepo(r.db, *blob, m.Repo)
			if err != nil {
				return nil, nil, err
			}
		}
	}

	//if the manifest is an image, we need to replicate the image configuration
	//blob immediately because ValidateAndStoreManifest() uses it for validation
	//purposes
	configBlobDesc := keppel.FindImageConfigBlob(manifestParsed)
	if configBlobDesc != nil {
		configBlob, err := keppel.FindBlobByAccountName(r.db, configBlobDesc.Digest, m.Account)
		if err != nil {
			return nil, nil, err
		}
		if configBlob.StorageID == "" {
			_, err = r.ReplicateBlob(*configBlob, m.Account, m.Repo, nil)
			if err != nil {
				return nil, nil, err
			}
		}
	}

	manifest, err := proc.ValidateAndStoreManifest(m.Account, m.Repo, processor.IncomingManifest{
		Reference: m.Reference,
		MediaType: manifestMediaType,
		Contents:  manifestBytes,
		PushedAt:  r.timeNow(),
	})
	return manifest, manifestBytes, err
}
