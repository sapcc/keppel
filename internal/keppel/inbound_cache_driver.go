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
)

//InboundCacheDriver is the abstract interface for a caching strategy for
//manifests and tags residing in an external registry.
type InboundCacheDriver interface {
	//LoadManifest pulls a manifest from the cache. If the given manifest is not
	//cached, or if the cache entry has expired, sql.ErrNoRows shall be returned.
	//
	//time.Now() is given in the second argument to allow for tests to use an
	//artificial wall clock.
	LoadManifest(location ImageReference, now time.Time) (contents []byte, mediaType string, err error)
	//StoreManifest places a manifest in the cache for later retrieval.
	//
	//time.Now() is given in the last argument to allow for tests to use an
	//artificial wall clock.
	StoreManifest(location ImageReference, contents []byte, mediaType string, now time.Time) error
}

var inboundCacheDriverFactories = make(map[string]func(Configuration) (InboundCacheDriver, error))

//NewInboundCacheDriver creates a new InboundCacheDriver using one of the
//factory functions registered with RegisterInboundCacheDriver().
func NewInboundCacheDriver(name string, cfg Configuration) (InboundCacheDriver, error) {
	factory := inboundCacheDriverFactories[name]
	if factory != nil {
		return factory(cfg)
	}
	return nil, errors.New("no such inbound cache driver: " + name)
}

//RegisterInboundCacheDriver registers an InboundCacheDriver. Call this from
//func init() of the package defining the InboundCacheDriver.
func RegisterInboundCacheDriver(name string, factory func(Configuration) (InboundCacheDriver, error)) {
	if _, exists := inboundCacheDriverFactories[name]; exists {
		panic("attempted to register multiple inbound cache drivers with name = " + name)
	}
	inboundCacheDriverFactories[name] = factory
}
