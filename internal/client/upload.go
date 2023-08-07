/******************************************************************************
*
*  Copyright 2020 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package client

import (
	"bytes"
	"context"
	"net/http"
	"strconv"

	"github.com/opencontainers/go-digest"
)

// UploadMonolithicBlob performs a monolithic blob upload. On success, the
// blob's digest is returned.
func (c *RepoClient) UploadMonolithicBlob(ctx context.Context, contents []byte) (digest.Digest, error) {
	d := digest.Canonical.FromBytes(contents)

	resp, err := c.doRequest(ctx, repoRequest{
		Method: "POST",
		Path:   "blobs/uploads/?digest=" + d.String(),
		Headers: http.Header{
			"Content-Type": {"application/octet-stream"},
		},
		Body:         bytes.NewReader(contents),
		ExpectStatus: http.StatusCreated,
	})
	if err == nil {
		resp.Body.Close()
	}
	return d, err
}

// UploadManifest uploads a manifest. If `tagName` is not empty, this tag name
// is used, otherwise the manifest is uploaded to its canonical digest. On
// success, the manifest's digest is returned.
func (c *RepoClient) UploadManifest(ctx context.Context, contents []byte, mediaType, tagName string) (digest.Digest, error) {
	d := digest.Canonical.FromBytes(contents)
	ref := tagName
	if tagName == "" {
		ref = d.String()
	}

	resp, err := c.doRequest(ctx, repoRequest{
		Method: "PUT",
		Path:   "manifests/" + ref,
		Headers: http.Header{
			"Content-Length": {strconv.Itoa(len(contents))},
			"Content-Type":   {mediaType},
		},
		Body:         bytes.NewReader(contents),
		ExpectStatus: http.StatusCreated,
	})
	if err == nil {
		resp.Body.Close()
	}
	return d, err
}
