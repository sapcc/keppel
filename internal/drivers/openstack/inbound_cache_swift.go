// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package openstack

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/majewsky/schwift/v2"

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
func (d *inboundCacheDriverSwift) Init(ctx context.Context, cfg keppel.Configuration) (err error) {
	d.HostInclusionRx, err = compileOptionalImplicitlyBoundedRegex(os.Getenv("KEPPEL_INBOUND_CACHE_ONLY_HOSTS"))
	if err != nil {
		return err
	}
	d.HostExclusionRx, err = compileOptionalImplicitlyBoundedRegex(os.Getenv("KEPPEL_INBOUND_CACHE_EXCEPT_HOSTS"))
	if err != nil {
		return err
	}
	d.Container, err = initSwiftContainerConnection(ctx, "KEPPEL_INBOUND_CACHE_")
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
// It is the callers responsibility to close the returned io.ReaderCloser.
func (d *inboundCacheDriverSwift) LoadManifest(ctx context.Context, location models.ImageReference, now time.Time) (contents io.ReadCloser, mediaType string, returnedError error) {
	if d.skip(location) {
		return nil, "", sql.ErrNoRows
	}

	defer func() {
		if returnedError != nil && !errors.Is(returnedError, sql.ErrNoRows) {
			returnedError = fmt.Errorf("while performing a lookup in the inbound cache: %w", returnedError)
		}
	}()

	obj := d.objectFor(location)

	contents, err := obj.Download(ctx, nil).AsReadCloser()
	if err != nil {
		if schwift.Is(err, http.StatusNotFound) {
			return nil, "", sql.ErrNoRows
		}
		return nil, "", err
	}

	hdr, err := obj.Headers(ctx) // NOTE: this does not actually make a HEAD request because we already did GET
	if err != nil {
		return nil, "", err
	}
	return contents, hdr.ContentType().Get(), nil
}

// StoreManifest implements the keppel.InboundCacheDriver interface.
func (d *inboundCacheDriverSwift) StoreManifest(ctx context.Context, location models.ImageReference, contents io.Reader, mediaType string, now time.Time) error {
	if d.skip(location) {
		return nil
	}

	hdr := schwift.NewObjectHeaders()
	hdr.ContentType().Set(mediaType)
	hdr.ExpiresAt().Set(d.expiryFor(location, now))

	obj := d.objectFor(location)
	err := obj.Upload(ctx, contents, nil, hdr.ToOpts())
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
			imageRef.Host, imageRef.RepoName, imageRef.Reference.Digest)
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
