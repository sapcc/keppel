// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package processor

import "github.com/opencontainers/go-digest"

// BytesWithDigest is a bytestring that already had its digest computed earlier.
// This type can be used if the digest of the bytestring is needed on multiple separate occasions.
type BytesWithDigest struct {
	bytes     []byte
	ownDigest digest.Digest
}

// NewBytesWithDigest constructs a BytesWithDigest from the input.
func NewBytesWithDigest(buf []byte) BytesWithDigest {
	return BytesWithDigest{
		bytes:     buf,
		ownDigest: digest.Canonical.FromBytes(buf),
	}
}

// Bytes returns the bytestring of this BytesWithDigest.
func (b BytesWithDigest) Bytes() []byte {
	return b.bytes
}

// Digest returns the digest of this BytesWithDigest.
func (b BytesWithDigest) Digest() digest.Digest {
	return b.ownDigest
}

// Len returns the length of the bytestring of this BytesWithDigest.
func (b BytesWithDigest) Len() int {
	return len(b.bytes)
}
