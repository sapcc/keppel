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
	"io/ioutil"
	"time"

	"github.com/docker/distribution"
	"github.com/sapcc/keppel/internal/keppel"
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

	//query upstream for the manifest
	manifestReader, _, manifestContentType, err := r.fetchFromUpstream(m.Repo, "GET", "manifests/"+m.Reference.String(), peer, peerToken)
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

	//validate manifest
	manifest, manifestDesc, err := distribution.UnmarshalManifest(manifestContentType, manifestBytes)
	if err != nil {
		return nil, nil, keppel.ErrManifestInvalid.With(err.Error())
	}
	//if <reference> is not a tag, it must be the digest of the manifest
	if m.Reference.IsDigest() && manifestDesc.Digest.String() != m.Reference.Digest.String() {
		return nil, nil, keppel.ErrDigestInvalid.With("upstream manifest digest is " + manifestDesc.Digest.String())
	}

	//NOTE: We trust upstream to have all blobs referenced by this manifest; these will
	//be replicated when a client first asks for them.

	//compute total size of image (TODO: code duplication with handlePutManifest())
	sizeBytes := uint64(manifestDesc.Size)
	for _, desc := range manifest.References() {
		sizeBytes += uint64(desc.Size)
	}

	tx, err := r.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer keppel.RollbackUnlessCommitted(tx)

	dbManifest := keppel.Manifest{
		RepositoryID: m.Repo.ID,
		Digest:       manifestDesc.Digest.String(),
		MediaType:    manifestDesc.MediaType,
		SizeBytes:    sizeBytes,
		PushedAt:     time.Now(),
	}
	err = dbManifest.InsertIfMissing(tx)
	if err != nil {
		return nil, nil, err
	}
	if m.Reference.IsTag() {
		err = keppel.Tag{
			RepositoryID: m.Repo.ID,
			Name:         m.Reference.Tag,
			Digest:       manifestDesc.Digest.String(),
			PushedAt:     time.Now(),
		}.InsertIfMissing(tx)
		if err != nil {
			return nil, nil, err
		}
	}

	//before committing, put the manifest into the backend
	err = r.sd.WriteManifest(m.Account, m.Repo.Name, manifestDesc.Digest.String(), manifestBytes)
	if err != nil {
		return nil, nil, err
	}

	return &dbManifest, manifestBytes, tx.Commit()
}
