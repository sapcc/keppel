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
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"
	"github.com/sapcc/keppel/internal/keppel"
)

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
func (c *RepoClient) ValidateManifest(reference string, logger ValidationLogger, platformFilter keppel.PlatformFilter) error {
	cache := make(map[string]error)
	if logger == nil {
		logger = noopLogger{}
	}
	return c.doValidateManifest(reference, 0, logger, platformFilter, cache)
}

func (c *RepoClient) doValidateManifest(reference string, level int, logger ValidationLogger, platformFilter keppel.PlatformFilter, cache map[string]error) (returnErr error) {
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

	manifestBytes, manifestMediaType, err := c.DownloadManifest(reference, nil)
	if err != nil {
		return err
	}
	manifest, manifestDesc, err := keppel.ParseManifest(manifestMediaType, manifestBytes)
	if err != nil {
		return err
	}

	//the manifest itself looks good...
	logger.LogManifest(manifestDesc.Digest.String(), level, nil)
	logged = true
	cache[manifestDesc.Digest.String()] = nil

	//...now recurse into the manifests and blobs that it references
	for _, desc := range manifest.BlobReferences() {
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
	for _, desc := range manifest.ManifestReferences(platformFilter) {
		err := c.doValidateManifest(desc.Digest.String(), level+1, logger, platformFilter, cache)
		if err != nil {
			return err
		}
	}

	return nil
}

//ValidateBlobContents fetches the given blob from the repo and verifies that
//the contents produce the correct digest.
func (c *RepoClient) ValidateBlobContents(blobDigest digest.Digest) (returnErr error) {
	readCloser, _, err := c.DownloadBlob(blobDigest)
	if err != nil {
		return err
	}

	defer func() {
		if returnErr == nil {
			returnErr = readCloser.Close()
		} else {
			readCloser.Close()
		}
	}()

	hash := blobDigest.Algorithm().Hash()
	_, err = io.Copy(hash, readCloser)
	if err != nil {
		return err
	}
	actualDigest := digest.NewDigest(blobDigest.Algorithm(), hash)
	if actualDigest != blobDigest {
		return fmt.Errorf("actual digest is %s", actualDigest)
	}
	return nil
}
