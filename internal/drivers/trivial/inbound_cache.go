// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package trivial

import (
	"context"
	"database/sql"
	"time"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

type inboundCacheDriver struct{}

func init() {
	keppel.InboundCacheDriverRegistry.Add(func() keppel.InboundCacheDriver { return inboundCacheDriver{} })
}

// PluginTypeID implements the keppel.InboundCacheDriver interface.
func (inboundCacheDriver) PluginTypeID() string { return driverName }

// Init implements the keppel.InboundCacheDriver interface.
func (inboundCacheDriver) Init(ctx context.Context, cfg keppel.Configuration) error {
	return nil
}

// LoadManifest implements the keppel.InboundCacheDriver interface.
func (inboundCacheDriver) LoadManifest(ctx context.Context, location models.ImageReference, now time.Time) (contents []byte, mediaType string, err error) {
	// always return a cache miss
	return nil, "", sql.ErrNoRows
}

// StoreManifest implements the keppel.InboundCacheDriver interface.
func (inboundCacheDriver) StoreManifest(ctx context.Context, location models.ImageReference, contents []byte, mediaType string, now time.Time) error {
	// no-op
	return nil
}
