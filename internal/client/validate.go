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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/docker/distribution"
	"github.com/opencontainers/go-digest"
	"github.com/sapcc/keppel/internal/keppel"
)

//RepoClient contains methods for interacting with a repository on a registry server.
type RepoClient struct {
	Host     string //either a plain hostname or a host:port like "example.org:443"
	RepoName string

	//credentials (only needed for non-public repos)
	UserName string
	Password string

	//auth state
	token string
}

//ValidationLogger can be passed to ValidateManifest, primarily to allow the
//caller to log the progress of the validation operation.
type ValidationLogger interface {
	LogManifest(reference string, level int, validationResult error)
	LogBlob(d digest.Digest, level int, validationResult error)
}

type noopLogger struct{}

func (noopLogger) LogManifest(string, int, error)    {}
func (noopLogger) LogBlob(digest.Digest, int, error) {}

//ValidateManifest fetches the given manifest from the repo and verifies that
//it parses correctly. It also validates all references manifests and blobs
//recursively.
func (c *RepoClient) ValidateManifest(reference string, logger ValidationLogger) error {
	cache := make(map[string]error)
	if logger == nil {
		logger = noopLogger{}
	}
	return c.doValidateManifest(reference, 0, logger, cache)
}

func (c *RepoClient) doValidateManifest(reference string, level int, logger ValidationLogger, cache map[string]error) (returnErr error) {
	if cachedResult, exists := cache[reference]; exists {
		logger.LogManifest(reference, level, cachedResult)
		return cachedResult
	}

	logged := false
	defer func() {
		if !logged {
			logger.LogManifest(reference, level, returnErr)
		}
	}()

	resp, err := c.doGetRequest("manifests/"+reference, http.Header{
		"Accept": distribution.ManifestMediaTypes(),
	})
	if err != nil {
		return err
	}
	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	err = resp.Body.Close()
	if err != nil {
		return err
	}

	manifest, manifestDesc, err := distribution.UnmarshalManifest(
		resp.Header.Get("Content-Type"),
		respBytes,
	)
	if err != nil {
		return err
	}

	//the manifest itself looks good...
	logger.LogManifest(manifestDesc.Digest.String(), level, nil)
	logged = true
	cache[manifestDesc.Digest.String()] = nil

	//...now recurse into the manifests and blobs that it references
	for _, desc := range manifest.References() {
		if isManifestMediaType(desc.MediaType) {
			err := c.doValidateManifest(desc.Digest.String(), level+1, logger, cache)
			if err != nil {
				return err
			}
		} else {
			if cachedResult, exists := cache[desc.Digest.String()]; exists {
				logger.LogBlob(desc.Digest, level+1, cachedResult)
			} else {
				err := c.ValidateBlobContents(desc.Digest)
				logger.LogBlob(desc.Digest, level+1, err)
				cache[desc.Digest.String()] = err
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

//ValidateBlobContents fetches the given blob from the repo and verifies that
//the contents produce the correct digest.
func (c *RepoClient) ValidateBlobContents(blobDigest digest.Digest) (returnErr error) {
	resp, err := c.doGetRequest("blobs/"+blobDigest.String(), nil)
	if err != nil {
		return err
	}

	defer func() {
		if returnErr == nil {
			returnErr = resp.Body.Close()
		} else {
			resp.Body.Close()
		}
	}()

	hash := blobDigest.Algorithm().Hash()
	_, err = io.Copy(hash, resp.Body)
	if err != nil {
		return err
	}
	actualDigest := digest.NewDigest(blobDigest.Algorithm(), hash)
	if actualDigest != blobDigest {
		return fmt.Errorf("actual digest is %s", actualDigest)
	}
	return nil
}

func (c *RepoClient) doGetRequest(path string, hdr http.Header) (*http.Response, error) {
	uri := fmt.Sprintf("https://%s/v2/%s/%s",
		c.Host, c.RepoName, path)

	//send GET request for manifest
	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range hdr {
		req.Header[k] = v
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	//if it's a 401, do the auth challenge...
	if resp.StatusCode == http.StatusUnauthorized {
		authChallenge, err := ParseAuthChallenge(resp.Header)
		if err != nil {
			return nil, fmt.Errorf("cannot parse auth challenge from 401 response to GET %s: %s", uri, err.Error())
		}
		c.token, err = authChallenge.GetToken(c.UserName, c.Password)
		if err != nil {
			return nil, fmt.Errorf("authentication failed: %s", err.Error())
		}

		//...then resend the GET request with the token
		req, err := http.NewRequest("GET", uri, nil)
		if err != nil {
			return nil, err
		}
		for k, v := range hdr {
			req.Header[k] = v
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
	}

	if resp.StatusCode != http.StatusOK {
		respBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		err = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		return nil, parseRegistryAPIError(respBytes)
	}

	return resp, nil
}

func parseRegistryAPIError(respBytes []byte) error {
	var data struct {
		Errors []*keppel.RegistryV2Error `json:"errors"`
	}
	err := json.Unmarshal(respBytes, &data)
	if err == nil {
		return data.Errors[0]
	}
	return errors.New(string(respBytes))
}

func isManifestMediaType(contentType string) bool {
	for _, mt := range distribution.ManifestMediaTypes() {
		if mt == contentType {
			return true
		}
	}
	return false
}
