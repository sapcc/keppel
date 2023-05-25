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
	"os"
	"regexp"
	"time"

	"github.com/majewsky/schwift"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

type inboundCacheDriverSwift struct {
	Container       *schwift.Container
	HostInclusionRx *regexp.Regexp
	HostExclusionRx *regexp.Regexp
}

func init() {
	keppel.InboundCacheDriverRegistry.Add(func() keppel.InboundCacheDriver { return &inboundCacheDriverSwift{} })
}

// PluginTypeID implements the keppel.InboundCacheDriver interface.
func (d *inboundCacheDriverSwift) PluginTypeID() string { return "swift" }

// Init implements the keppel.InboundCacheDriver interface.
func (d *inboundCacheDriverSwift) Init(cfg keppel.Configuration) (err error) {
	d.HostInclusionRx, err = compileOptionalImplicitlyBoundedRegex(os.Getenv("KEPPEL_INBOUND_CACHE_ONLY_HOSTS"))
	if err != nil {
		return err
	}
	d.HostExclusionRx, err = compileOptionalImplicitlyBoundedRegex(os.Getenv("KEPPEL_INBOUND_CACHE_EXCEPT_HOSTS"))
	if err != nil {
		return err
	}
	d.Container, err = initSwiftContainerConnection("KEPPEL_INBOUND_CACHE_")
	return err
}

func compileOptionalImplicitlyBoundedRegex(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}

	rx, err := regexp.Compile(`^(?:` + pattern + `)$`)
	if err != nil {
		return nil, fmt.Errorf("%q is not a valid regex: %w", pattern, err)
	}
	return rx, nil
}

// LoadManifest implements the keppel.InboundCacheDriver interface.
func (d *inboundCacheDriverSwift) LoadManifest(location models.ImageReference, now time.Time) (contents []byte, mediaType string, returnedError error) {
	if d.skip(location) {
		return nil, "", sql.ErrNoRows
	}

	defer func() {
		if returnedError != nil && returnedError != sql.ErrNoRows {
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

// StoreManifest implements the keppel.InboundCacheDriver interface.
func (d *inboundCacheDriverSwift) StoreManifest(location models.ImageReference, contents []byte, mediaType string, now time.Time) error {
	if d.skip(location) {
		return nil
	}

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

func (d *inboundCacheDriverSwift) objectFor(imageRef models.ImageReference) *schwift.Object {
	var name string
	if imageRef.Reference.IsTag() {
		name = fmt.Sprintf("%s/%s/_tags/%s",
			imageRef.Host, imageRef.RepoName, imageRef.Reference.Tag)
	} else {
		name = fmt.Sprintf("%s/%s/_manifests/%s",
			imageRef.Host, imageRef.RepoName, imageRef.Reference.Digest.String())
	}
	return d.Container.Object(name)
}

func (d *inboundCacheDriverSwift) expiryFor(imageRef models.ImageReference, now time.Time) time.Time {
	if imageRef.Reference.IsTag() {
		return now.Add(3 * time.Hour)
	}
	return now.Add(48 * time.Hour)
}

func (d *inboundCacheDriverSwift) skip(imageRef models.ImageReference) bool {
	if d.HostInclusionRx != nil && !d.HostInclusionRx.MatchString(imageRef.Host) {
		return true
	}
	if d.HostExclusionRx != nil && d.HostExclusionRx.MatchString(imageRef.Host) {
		return true
	}
	return false
}
