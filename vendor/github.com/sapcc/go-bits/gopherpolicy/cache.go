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

package gopherpolicy

import (
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
	//lru.New() only fails if a non-negative size is given, so it's safe to
	//ignore the error here
	//nolint:errcheck
	c, _ := lru.New[string, []byte](256)
	return inMemoryCacher{c}
}

func (c inMemoryCacher) StoreTokenPayload(token string, payload []byte) {
	c.Add(cacheKeyFor(token), payload)
}

func (c inMemoryCacher) LoadTokenPayload(token string) []byte {
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
