// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"context"
	"time"

	"github.com/sapcc/go-bits/pluggable"

	"github.com/sapcc/keppel/internal/models"
)

// InboundCacheDriver is the abstract interface for a caching strategy for
// manifests and tags residing in an external registry.
type InboundCacheDriver interface {
	pluggable.Plugin
	// Init is called before any other interface methods, and allows the plugin to
	// perform first-time initialization.
	Init(context.Context, Configuration) error

	// LoadManifest pulls a manifest from the cache. If the given manifest is not
	// cached, or if the cache entry has expired, sql.ErrNoRows shall be returned.
	//
	// time.Now() is given in the second argument to allow for tests to use an
	// artificial wall clock.
	LoadManifest(ctx context.Context, location models.ImageReference, now time.Time) (contents []byte, mediaType string, err error)
	// StoreManifest places a manifest in the cache for later retrieval.
	//
	// time.Now() is given in the last argument to allow for tests to use an
	// artificial wall clock.
	StoreManifest(ctx context.Context, location models.ImageReference, contents []byte, mediaType string, now time.Time) error
}

// InboundCacheDriverRegistry is a pluggable.Registry for InboundCacheDriver implementations.
var InboundCacheDriverRegistry pluggable.Registry[InboundCacheDriver]

// NewInboundCacheDriver creates a new InboundCacheDriver using one of the
// plugins registered with InboundCacheDriverRegistry.
//
// The supplied config must be a string of the form {"type":"foobar","params":{...}},
// where `type` is the plugin type ID and `params` is json.Unmarshal()ed into
// the driver instance to supply driver-specific configuration.
func NewInboundCacheDriver(ctx context.Context, configJSON string, cfg Configuration) (InboundCacheDriver, error) {
	callInit := func(icd InboundCacheDriver) error {
		return icd.Init(ctx, cfg)
	}
	return newDriver("KEPPEL_DRIVER_INBOUND_CACHE", InboundCacheDriverRegistry, configJSON, callInit)
}
