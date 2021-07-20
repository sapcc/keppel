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
	"database/sql"
	"time"

	"github.com/sapcc/keppel/internal/keppel"
)

type inboundCacheDriver struct{}

func init() {
	keppel.RegisterInboundCacheDriver("trivial", func(_ keppel.Configuration) (keppel.InboundCacheDriver, error) {
		return inboundCacheDriver{}, nil
	})
}

//LoadManifest implements the keppel.InboundCacheDriver interface.
func (inboundCacheDriver) LoadManifest(location keppel.InboundCacheLocation, now time.Time) (contents []byte, mediaType string, err error) {
	//always return a cache miss
	return nil, "", sql.ErrNoRows
}

//StoreManifest implements the keppel.InboundCacheDriver interface.
func (inboundCacheDriver) StoreManifest(location keppel.InboundCacheLocation, contents []byte, mediaType string, now time.Time) error {
	//no-op
	return nil
}
