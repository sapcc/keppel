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

package keppel

import (
	"errors"
	"time"

	"github.com/sapcc/go-bits/pluggable"

	"github.com/sapcc/keppel/internal/models"
)

// InboundCacheDriver is the abstract interface for a caching strategy for
// manifests and tags residing in an external registry.
type InboundCacheDriver interface {
	pluggable.Plugin
	//Init is called before any other interface methods, and allows the plugin to
	//perform first-time initialization.
	Init(Configuration) error

	//LoadManifest pulls a manifest from the cache. If the given manifest is not
	//cached, or if the cache entry has expired, sql.ErrNoRows shall be returned.
	//
	//time.Now() is given in the second argument to allow for tests to use an
	//artificial wall clock.
	LoadManifest(location models.ImageReference, now time.Time) (contents []byte, mediaType string, err error)
	//StoreManifest places a manifest in the cache for later retrieval.
	//
	//time.Now() is given in the last argument to allow for tests to use an
	//artificial wall clock.
	StoreManifest(location models.ImageReference, contents []byte, mediaType string, now time.Time) error
}

// InboundCacheDriverRegistry is a pluggable.Registry for InboundCacheDriver implementations.
var InboundCacheDriverRegistry pluggable.Registry[InboundCacheDriver]

// NewInboundCacheDriver creates a new InboundCacheDriver using one of the
// plugins registered with InboundCacheDriverRegistry.
func NewInboundCacheDriver(pluginTypeID string, cfg Configuration) (InboundCacheDriver, error) {
	icd := InboundCacheDriverRegistry.Instantiate(pluginTypeID)
	if icd == nil {
		return nil, errors.New("no such inbound cache driver: " + pluginTypeID)
	}
	return icd, icd.Init(cfg)
}
