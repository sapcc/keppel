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
func (inboundCacheDriver) PluginTypeID() string { return "trivial" }

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
