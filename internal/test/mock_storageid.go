// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// StorageIDGenerator provides realistic-looking, but deterministic storage IDs
// for unit tests.
type StorageIDGenerator struct {
	n uint64
}

// Next returns the next storage ID.
func (g *StorageIDGenerator) Next() string {
	g.n++
	return storageIDFor(g.n)
}

// Peek returns the storage ID that would be produced by the n-th call to Next
// (starting at n=1), without advancing the generator. This is useful when
// asserting DB contents that were populated by production code which already
// consumed the generator.
func (g *StorageIDGenerator) Peek(n uint64) string {
	return storageIDFor(n)
}

func storageIDFor(n uint64) string {
	inputStr := strconv.FormatUint(n, 10)
	// SHA-256 gives 32 bytes of "randomness", same as keppel.GenerateStorageID()
	hashBytes := sha256.Sum256([]byte(inputStr))
	return hex.EncodeToString(hashBytes[:])
}
