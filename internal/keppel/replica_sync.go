/*******************************************************************************
*
* Copyright 2021 SAP SE
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

package keppel

//ReplicaSyncPayload is the format for request bodies and response bodies of
//the sync-replica API endpoint.
//
//(This type is declared in this package because it gets used in both
//internal/api/peer and internal/tasks.)
type ReplicaSyncPayload struct {
	Manifests []ManifestForSync `json:"manifests"`
}

//ManifestForSync represents a manifest in the _sync_replica API endpoint.
//
//(This type is declared in this package because it gets used in both
//internal/api/peer and internal/tasks.)
type ManifestForSync struct {
	Digest       string       `json:"digest"`
	LastPulledAt *int64       `json:"last_pulled_at,omitempty"`
	Tags         []TagForSync `json:"tags,omitempty"`
}

//TagForSync represents a tag in the _sync_replica API endpoint.
//
//(This type is declared in this package because it gets used in both
//internal/api/peer and internal/tasks.)
type TagForSync struct {
	Name         string `json:"name"`
	LastPulledAt *int64 `json:"last_pulled_at,omitempty"`
}

//HasManifest returns whether there is a manifest with the given digest in this
//payload.
func (p ReplicaSyncPayload) HasManifest(digest string) bool {
	for _, m := range p.Manifests {
		if m.Digest == digest {
			return true
		}
	}
	return false
}

//DigestForTag returns the digest of the manifest that this tag points to, or
//the empty string if the tag does not exist in this payload.
func (p ReplicaSyncPayload) DigestForTag(name string) string {
	for _, m := range p.Manifests {
		for _, t := range m.Tags {
			if t.Name == name {
				return m.Digest
			}
		}
	}
	return ""
}
