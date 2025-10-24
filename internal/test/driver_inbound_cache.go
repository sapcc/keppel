// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"time"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// InboundCacheDriver (driver ID "unittest") is a keppel.InboundCacheDriver for
// unit tests. It remembers all manifests ever pushed into it in-memory (which
// is a really bad idea for an production driver because of the potentially
// unbounded memory footprint).
type InboundCacheDriver struct {
	MaxAge  time.Duration
	Entries map[models.ImageReference]inboundCacheEntry
}

type inboundCacheEntry struct {
	Contents   []byte
	MediaType  string
	InsertedAt time.Time
}

func init() {
	keppel.InboundCacheDriverRegistry.Add(func() keppel.InboundCacheDriver { return &InboundCacheDriver{} })
}

// PluginTypeID implements the keppel.InboundCacheDriver interface.
func (d *InboundCacheDriver) PluginTypeID() string { return "unittest" }

// Init implements the keppel.InboundCacheDriver interface.
func (d *InboundCacheDriver) Init(ctx context.Context, cfg keppel.Configuration) error {
	d.MaxAge = 6 * time.Hour
	d.Entries = make(map[models.ImageReference]inboundCacheEntry)
	return nil
}

// LoadManifest implements the keppel.InboundCacheDriver interface.
func (d *InboundCacheDriver) LoadManifest(ctx context.Context, location models.ImageReference, now time.Time) (contents io.ReadCloser, mediaType string, err error) {
	maxInsertedAt := now.Add(-d.MaxAge)
	entry, ok := d.Entries[location]
	if ok && entry.InsertedAt.After(maxInsertedAt) {
		return io.NopCloser(bytes.NewReader(entry.Contents)), entry.MediaType, nil
	}
	return nil, "", sql.ErrNoRows
}

// StoreManifest implements the keppel.InboundCacheDriver interface.
func (d *InboundCacheDriver) StoreManifest(ctx context.Context, location models.ImageReference, in io.Reader, mediaType string, now time.Time) error {
	contents, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	d.Entries[location] = inboundCacheEntry{contents, mediaType, now}
	return nil
}
