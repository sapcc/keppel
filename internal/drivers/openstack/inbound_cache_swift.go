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

package openstack

import (
	"bytes"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/majewsky/schwift"
	"github.com/sapcc/keppel/internal/keppel"
)

type inboundCacheDriverSwift struct {
	Container *schwift.Container
}

func init() {
	keppel.RegisterInboundCacheDriver("swift", func(_ keppel.Configuration) (keppel.InboundCacheDriver, error) {
		container, err := initSwiftContainerConnection("KEPPEL_INBOUND_CACHE_")
		return &inboundCacheDriverSwift{Container: container}, err
	})
}

//LoadManifest implements the keppel.InboundCacheDriver interface.
func (d *inboundCacheDriverSwift) LoadManifest(location keppel.InboundCacheLocation, now time.Time) (contents []byte, mediaType string, returnedError error) {
	defer func() {
		if returnedError != nil {
			returnedError = fmt.Errorf("while performing a lookup in the inbound cache: %w", returnedError)
		}
	}()

	obj := d.objectFor(location)

	contents, err := obj.Download(nil).AsByteSlice()
	if err != nil {
		if schwift.Is(err, http.StatusNotFound) {
			return nil, "", sql.ErrNoRows
		}
		return nil, "", err
	}

	hdr, err := obj.Headers() // NOTE: this does not actually make a HEAD request because we already did GET
	if err != nil {
		return nil, "", err
	}
	return contents, hdr.ContentType().Get(), nil
}

//StoreManifest implements the keppel.InboundCacheDriver interface.
func (d *inboundCacheDriverSwift) StoreManifest(location keppel.InboundCacheLocation, contents []byte, mediaType string, now time.Time) error {
	hdr := schwift.NewObjectHeaders()
	hdr.ContentType().Set(mediaType)
	hdr.ExpiresAt().Set(d.expiryFor(location, now))

	obj := d.objectFor(location)
	err := obj.Upload(bytes.NewReader(contents), nil, hdr.ToOpts())
	if err != nil {
		return fmt.Errorf("while populating the inbound cache: %w", err)
	}
	return nil
}

func (d *inboundCacheDriverSwift) objectFor(loc keppel.InboundCacheLocation) *schwift.Object {
	var name string
	if loc.Reference.IsTag() {
		name = fmt.Sprintf("%s/%s/_tags/%s",
			loc.HostName, loc.RepoName, loc.Reference.Tag)
	} else {
		name = fmt.Sprintf("%s/%s/_manifests/%s",
			loc.HostName, loc.RepoName, loc.Reference.Digest.String())
	}
	return d.Container.Object(name)
}

func (d *inboundCacheDriverSwift) expiryFor(loc keppel.InboundCacheLocation, now time.Time) time.Time {
	if loc.Reference.IsTag() {
		return now.Add(3 * time.Hour)
	}
	return now.Add(48 * time.Hour)
}
