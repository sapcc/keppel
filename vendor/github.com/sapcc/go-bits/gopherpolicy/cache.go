// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package gopherpolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	lru "github.com/hashicorp/golang-lru/v2"
)

type inMemoryCacher struct {
	*lru.Cache[string, []byte]
}

// InMemoryCacher builds a Cacher that stores token payloads in memory. At most
// 256 token payloads will be cached, so this will never use more than 4-8 MiB
// of memory.
func InMemoryCacher() Cacher {
	// lru.New() only fails if a non-negative size is given, so it's safe to
	// ignore the error here
	//nolint:errcheck
	c, _ := lru.New[string, []byte](256)
	return inMemoryCacher{c}
}

func (c inMemoryCacher) StoreTokenPayload(_ context.Context, token string, payload []byte) {
	c.Add(cacheKeyFor(token), payload)
}

func (c inMemoryCacher) LoadTokenPayload(_ context.Context, token string) []byte {
	payload, ok := c.Get(cacheKeyFor(token))
	if !ok {
		return nil
	}
	return payload
}

func cacheKeyFor(token string) string {
	sha256Hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sha256Hash[:])
}
