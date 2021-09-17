/*******************************************************************************
*
* Copyright 2021 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package test

import (
	"database/sql"
	"time"

	"github.com/sapcc/keppel/internal/keppel"
)

//InboundCacheDriver (driver ID "unittest") is a keppel.InboundCacheDriver for
//unit tests. It remembers all manifests ever pushed into it in-memory (which
//is a really bad idea for an production driver because of the potentially
//unbounded memory footprint).
type InboundCacheDriver struct {
	MaxAge  time.Duration
	Entries map[keppel.ImageReference]inboundCacheEntry
}

type inboundCacheEntry struct {
	Contents   []byte
	MediaType  string
	InsertedAt time.Time
}

func init() {
	keppel.RegisterInboundCacheDriver("unittest", func(_ keppel.Configuration) (keppel.InboundCacheDriver, error) {
		defaultMaxAge := 6 * time.Hour
		return &InboundCacheDriver{defaultMaxAge, make(map[keppel.ImageReference]inboundCacheEntry)}, nil
	})
}

//LoadManifest implements the keppel.InboundCacheDriver interface.
func (d *InboundCacheDriver) LoadManifest(location keppel.ImageReference, now time.Time) (contents []byte, mediaType string, err error) {
	maxInsertedAt := now.Add(-d.MaxAge)
	entry, ok := d.Entries[location]
	if ok && entry.InsertedAt.After(maxInsertedAt) {
		return entry.Contents, entry.MediaType, nil
	}
	return nil, "", sql.ErrNoRows
}

//StoreManifest implements the keppel.InboundCacheDriver interface.
func (d *InboundCacheDriver) StoreManifest(location keppel.ImageReference, contents []byte, mediaType string, now time.Time) error {
	d.Entries[location] = inboundCacheEntry{contents, mediaType, now}
	return nil
}
