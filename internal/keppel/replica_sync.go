// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	. "github.com/majewsky/gg/option"
	"github.com/opencontainers/go-digest"
)

// ReplicaSyncPayload is the format for request bodies and response bodies of
// the sync-replica API endpoint.
//
// (This type is declared in this package because it gets used in both
// internal/api/peer and internal/tasks.)
type ReplicaSyncPayload struct {
	Manifests []ManifestForSync `json:"manifests"`
}

// ManifestForSync represents a manifest in the _sync_replica API endpoint.
//
// (This type is declared in this package because it gets used in both
// internal/api/peer and internal/tasks.)
type ManifestForSync struct {
	Digest       digest.Digest `json:"digest"`
	LastPulledAt Option[int64] `json:"last_pulled_at,omitzero"`
	Tags         []TagForSync  `json:"tags,omitempty"`
}

// TagForSync represents a tag in the _sync_replica API endpoint.
//
// (This type is declared in this package because it gets used in both
// internal/api/peer and internal/tasks.)
type TagForSync struct {
	Name         string        `json:"name"`
	LastPulledAt Option[int64] `json:"last_pulled_at,omitzero"`
}

// HasManifest returns whether there is a manifest with the given digest in this
// payload.
func (p ReplicaSyncPayload) HasManifest(manifestDigest digest.Digest) bool {
	for _, m := range p.Manifests {
		if m.Digest == manifestDigest {
			return true
		}
	}
	return false
}

// DigestForTag returns the digest of the manifest that this tag points to, or
// the empty string if the tag does not exist in this payload.
func (p ReplicaSyncPayload) DigestForTag(name string) digest.Digest {
	for _, m := range p.Manifests {
		for _, t := range m.Tags {
			if t.Name == name {
				return m.Digest
			}
		}
	}
	return ""
}
