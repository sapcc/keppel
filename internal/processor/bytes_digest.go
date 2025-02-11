/******************************************************************************
*
*  Copyright 2025 SAP SE
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
