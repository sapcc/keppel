// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/opencontainers/go-digest"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// DownloadBlob fetches a blob's contents from this repository. If an error is
// returned, it's usually a *keppel.RegistryV2Error.
func (c *RepoClient) DownloadBlob(ctx context.Context, blobDigest digest.Digest) (contents io.ReadCloser, sizeBytes uint64, returnErr error) {
	resp, err := c.doRequest(ctx, repoRequest{
		Method:       "GET",
		Path:         "blobs/" + blobDigest.String(),
		ExpectStatus: http.StatusOK,
	})
	if err != nil {
		return nil, 0, err
	}
	sizeBytes, err = strconv.ParseUint(resp.Header.Get("Content-Length"), 10, 64)
	if err != nil {
		resp.Body.Close()
		return nil, 0, err
	}
	return resp.Body, sizeBytes, nil
}

// DownloadManifestOpts appears in func DownloadManifest.
type DownloadManifestOpts struct {
	DoNotCountTowardsLastPulled bool
	ExtraHeaders                http.Header
}

// DownloadManifest fetches a manifest from this repository.
// If an error is returned, it's usually a *keppel.RegistryV2Error.
// The caller is responsible for closing the returned ReadCloser.
func (c *RepoClient) DownloadManifest(ctx context.Context, reference models.ManifestReference, opts *DownloadManifestOpts) (contents io.ReadCloser, mediaType string, returnErr error) {
	if opts == nil {
		opts = &DownloadManifestOpts{}
	}

	hdr := make(http.Header)
	hdr.Set("Accept", strings.Join(keppel.ManifestMediaTypes, ", "))
	if opts.DoNotCountTowardsLastPulled {
		hdr.Set("X-Keppel-No-Count-Towards-Last-Pulled", "1")
	}
	for k, v := range opts.ExtraHeaders {
		if len(v) > 0 {
			hdr[k] = v
		}
	}

	resp, err := c.doRequest(ctx, repoRequest{
		Method:       "GET",
		Path:         "manifests/" + reference.String(),
		Headers:      hdr,
		ExpectStatus: http.StatusOK,
	})
	if err != nil {
		return nil, "", err
	}

	return resp.Body, resp.Header.Get("Content-Type"), nil
}
