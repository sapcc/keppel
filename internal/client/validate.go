// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// ValidationLogger can be passed to ValidateManifest, primarily to allow the
// caller to log the progress of the validation operation.
type ValidationLogger interface {
	LogManifest(reference models.ManifestReference, level int, validationResult error, resultFromCache bool)
	LogBlob(d digest.Digest, level int, validationResult error, resultFromCache bool)
}

type noopLogger struct{}

func (noopLogger) LogManifest(models.ManifestReference, int, error, bool) {}
func (noopLogger) LogBlob(digest.Digest, int, error, bool)                {}

// ValidationSession holds state and caches intermediate results over the
// course of several ValidateManifest() and ValidateBlobContents() calls.
// The cache optimizes the validation of submanifests and blobs that are
// referenced multiple times. The session instance should only be used for as
// long as the caller wishes to cache validation results.
type ValidationSession struct {
	Logger  ValidationLogger
	isValid map[string]bool
}

func (s *ValidationSession) applyDefaults() *ValidationSession {
	if s == nil {
		// This branch is taken when the caller supplied `nil` for the
		// *ValidationSession argument in ValidateManifest or ValidateBlobContents.
		s = &ValidationSession{}
	}
	if s.Logger == nil {
		s.Logger = noopLogger{}
	}
	if s.isValid == nil {
		s.isValid = make(map[string]bool)
	}
	return s
}

func (c *RepoClient) validationCacheKey(digestOrTagName string) string {
	// We allow sharing a ValidationSession between multiple RepoClients to keep
	// the API simple. But we cannot share validation results between repos: For
	// any given digest, validation could succeed in one repo, fail in a second
	// repo, and fail *in a different way* in the third repo. Therefore we need
	// to store validation results keyed by digest *and* repo URL.
	return fmt.Sprintf("%s/%s/%s", c.Host, c.RepoName, digestOrTagName)
}

// ValidateManifest fetches the given manifest from the repo and verifies that
// it parses correctly. It also validates all references manifests and blobs
// recursively.
func (c *RepoClient) ValidateManifest(ctx context.Context, reference models.ManifestReference, session *ValidationSession, platformFilter models.PlatformFilter) error {
	return c.doValidateManifest(ctx, reference, 0, session.applyDefaults(), platformFilter)
}

func (c *RepoClient) doValidateManifest(ctx context.Context, reference models.ManifestReference, level int, session *ValidationSession, platformFilter models.PlatformFilter) (returnErr error) {
	if session.isValid[c.validationCacheKey(reference.String())] {
		session.Logger.LogManifest(reference, level, nil, true)
		return nil
	}

	logged := false
	defer func() {
		if !logged {
			session.Logger.LogManifest(reference, level, returnErr, false)
		}
	}()

	manifestBytes, manifestMediaType, err := c.DownloadManifest(ctx, reference, nil)
	if err != nil {
		return err
	}
	manifest, err := keppel.ParseManifest(manifestMediaType, manifestBytes)
	if err != nil {
		return err
	}

	manifestDigest := digest.FromBytes(manifestBytes)

	if reference.Digest != "" && manifestDigest != reference.Digest {
		return keppel.ErrDigestInvalid.With("actual manifest digest is " + manifestDigest.String())
	}

	// the manifest itself looks good...
	session.Logger.LogManifest(models.ManifestReference{Digest: manifestDigest}, level, nil, false)
	logged = true

	// ...now recurse into the manifests and blobs that it references
	for _, layerInfo := range manifest.BlobReferences() {
		err := c.doValidateBlobContents(ctx, layerInfo.Digest, level+1, session)
		if err != nil {
			return err
		}
	}
	for _, desc := range manifest.ManifestReferences(platformFilter) {
		err := c.doValidateManifest(ctx, models.ManifestReference{Digest: desc.Digest}, level+1, session, platformFilter)
		if err != nil {
			return err
		}
	}

	// write validity into cache only after all references have been validated as well
	session.isValid[c.validationCacheKey(manifestDigest.String())] = true
	session.isValid[c.validationCacheKey(reference.String())] = true
	return nil
}

// ValidateBlobContents fetches the given blob from the repo and verifies that
// the contents produce the correct digest.
func (c *RepoClient) ValidateBlobContents(ctx context.Context, blobDigest digest.Digest, session *ValidationSession) error {
	return c.doValidateBlobContents(ctx, blobDigest, 0, session.applyDefaults())
}

func (c *RepoClient) doValidateBlobContents(ctx context.Context, blobDigest digest.Digest, level int, session *ValidationSession) (returnErr error) {
	cacheKey := c.validationCacheKey(blobDigest.String())
	if session.isValid[cacheKey] {
		session.Logger.LogBlob(blobDigest, level, nil, true)
		return nil
	}
	defer func() {
		session.Logger.LogBlob(blobDigest, level, returnErr, false)
	}()

	readCloser, _, err := c.DownloadBlob(ctx, blobDigest)
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

	session.isValid[cacheKey] = true
	return nil
}
