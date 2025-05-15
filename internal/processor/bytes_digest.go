// SPDX-FileCopyrightText: 2025 SAP SE
// SPDX-License-Identifier: Apache-2.0

package processor

import "github.com/opencontainers/go-digest"

// BytesWithDigest is a bytestring that already had its digest computed earlier.
// This type can be used if the digest of the bytestring is needed on multiple separate occasions.
type BytesWithDigest struct {
	bytes     []byte
	ownDigest digest.Digest
}

func NewBytesWithDigest(buf []byte) BytesWithDigest {
	return BytesWithDigest{
		bytes:     buf,
		ownDigest: digest.Canonical.FromBytes(buf),
	}
}
func (b BytesWithDigest) Bytes() []byte {
	return b.bytes
}
func (b BytesWithDigest) Digest() digest.Digest {
	return b.ownDigest
}
func (b BytesWithDigest) Len() int {
	return len(b.bytes)
}
