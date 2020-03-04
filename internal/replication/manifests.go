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

	proc := processor.New(r.db, r.sd)
	manifest, err := proc.ValidateAndStoreManifest(m.Account, m.Repo, processor.IncomingManifest{
		Reference: m.Reference,
		MediaType: manifestMediaType,
		Contents:  manifestBytes,
		PushedAt:  time.Now(),
	})
	return manifest, manifestBytes, err
}
