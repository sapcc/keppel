// SPDX-FileCopyrightText: 2020 SAP SE
// SPDX-License-Identifier: Apache-2.0

package openstack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
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

func (c redisCacher) StoreTokenPayload(ctx context.Context, cacheKey string, payload []byte) {
	err := c.Set(ctx, hashCacheKey(cacheKey), payload, 5*time.Minute).Err()
	if err != nil {
		logg.Error("cannot cache token payload in Redis: %s", err.Error())
	}
}

func (c redisCacher) LoadTokenPayload(ctx context.Context, cacheKey string) []byte {
	payload, err := c.Get(ctx, hashCacheKey(cacheKey)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		logg.Error("cannot retrieve token payload from Redis: %s", err.Error())
		return nil
	}
	return payload
}
