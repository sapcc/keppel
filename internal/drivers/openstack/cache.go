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

package openstack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/sapcc/go-bits/logg"
)

// redisCacher is an adapter around *redis.Client that implements the
// gopherpolicy.Cacher interface.
type redisCacher struct {
	*redis.Client
}

func hashCacheKey(cacheKey string) string {
	sha256Hash := sha256.Sum256([]byte(cacheKey))
	return "keystone-" + hex.EncodeToString(sha256Hash[:])
}

func (c redisCacher) StoreTokenPayload(cacheKey string, payload []byte) {
	err := c.Set(context.Background(), hashCacheKey(cacheKey), payload, 5*time.Minute).Err()
	if err != nil {
		logg.Error("cannot cache token payload in Redis: %s", err.Error())
	}
}

func (c redisCacher) LoadTokenPayload(cacheKey string) []byte {
	payload, err := c.Get(context.Background(), hashCacheKey(cacheKey)).Bytes()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		logg.Error("cannot retrieve token payload from Redis: %s", err.Error())
		return nil
	}
	return payload
}
