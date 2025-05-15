// SPDX-FileCopyrightText: 2021 SAP SE
// SPDX-License-Identifier: Apache-2.0

package models

import "github.com/opencontainers/go-digest"

// ManifestReference is a reference to a manifest as encountered in a URL on the
// Registry v2 API. Exactly one of the members will be non-empty.
type ManifestReference struct {
	Digest digest.Digest
	Tag    string
}

// ParseManifestReference parses a manifest reference. If `reference` parses as
// a digest, it will be interpreted as a digest. Otherwise it will be
// interpreted as a tag name.
func ParseManifestReference(reference string) ManifestReference {
	parsedDigest, err := digest.Parse(reference)
	if err == nil {
		return ManifestReference{Digest: parsedDigest}
	}
	return ManifestReference{Tag: reference}
}

// String returns the original string representation of this reference.
func (r ManifestReference) String() string {
	if r.Digest != "" {
		return r.Digest.String()
	}
	return r.Tag
}

// IsDigest returns whether this reference is to a specific digest, rather than to a tag.
func (r ManifestReference) IsDigest() bool {
	return r.Digest != ""
}

// IsTag returns whether this reference is to a tag, rather than to a specific digest.
func (r ManifestReference) IsTag() bool {
	return r.Digest == ""
}
