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
	"io"
	"io/ioutil"
	"net/http"

	"github.com/docker/distribution"
	"github.com/opencontainers/go-digest"
)

//DownloadBlob fetches a blob's contents from this repository. If an error is
//returned, it's usually a *keppel.RegistryV2Error.
func (c *RepoClient) DownloadBlob(blobDigest digest.Digest) (io.ReadCloser, error) {
	resp, err := c.doRequest(repoRequest{
		Method:       "GET",
		Path:         "blobs/" + blobDigest.String(),
		ExpectStatus: http.StatusOK,
	})
	return resp.Body, err
}

//DownloadManifest fetches a manifest from this repository. If an error is
//returned, it's usually a *keppel.RegistryV2Error.
func (c *RepoClient) DownloadManifest(reference string) (contents []byte, mediaType string, returnErr error) {
	resp, err := c.doRequest(repoRequest{
		Method:       "GET",
		Path:         "manifests/" + reference,
		Headers:      http.Header{"Accept": distribution.ManifestMediaTypes()},
		ExpectStatus: http.StatusOK,
	})
	if err != nil {
		return nil, "", err
	}

	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	err = resp.Body.Close()
	if err != nil {
		return nil, "", err
	}

	return respBytes, resp.Header.Get("Content-Type"), nil
}
