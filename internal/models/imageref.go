// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package models

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/opencontainers/go-digest"
)

const (
	defaultHostName = "registry-1.docker.io"
	defaultTagName  = "latest"
)

// ImageReference refers to an image that can be pulled from a registry.
type ImageReference struct {
	Host      string // either a plain hostname or a host:port like "example.org:443"
	RepoName  RepositoryName
	Reference ManifestReference
}

// String returns the most compact string representation of this reference.
func (r ImageReference) String() string {
	var result string
	if r.Reference.IsDigest() {
		// digests are appended with "@"
		result = fmt.Sprintf("%s@%s", r.RepoName, r.Reference.Digest)
	} else {
		// tag names are appended with ":"
		if r.Reference.Tag == defaultTagName {
			result = string(r.RepoName)
		} else {
			result = fmt.Sprintf("%s:%s", r.RepoName, r.Reference.Tag)
		}
	}

	if r.Host == defaultHostName {
		// strip leading "library/" from repo name; e.g.
		// "registry-1.docker.io/library/alpine:3.9" becomes just "alpine:3.9"
		return strings.TrimPrefix(result, "library/")
	}
	return fmt.Sprintf("%s/%s", r.Host, result)
}

// ParseImageReference parses an image reference string like
// "registry.example.org/alpine:3.9" into an ImageReference struct.
// Both on success and on error, an additional string is returned indicating how
// the input was interpreted (e.g. which defaults were inferred). This can be
// shown to the user to help them understand how the reference was parsed.
func ParseImageReference(input string) (ImageReference, string, error) {
	// prepend hostname for default registry if input does not include a hostname or host:port
	inputParts := strings.SplitN(input, "/", 2)
	hadNoHostName := false
	if len(inputParts) == 1 || !looksLikeHostName(inputParts[0]) {
		input = fmt.Sprintf("%s/%s", defaultHostName, input)
		hadNoHostName = true
	}

	// reformat into a URL for parsing purposes
	input = "docker-pullable://" + input
	imageURL, err := url.Parse(input)
	if err != nil {
		return ImageReference{}, input, err
	}

	var (
		rawRepoName string // not yet confirmed to be a RepositoryName
		manifestRef ManifestReference
	)
	switch {
	case strings.Contains(imageURL.Path, "@"):
		// input references a digest
		pathParts := imageReferenceRx.FindStringSubmatch(imageURL.Path)
		if len(pathParts) < 1 {
			return ImageReference{}, input, fmt.Errorf("invalid image reference: %q", imageURL.Path)
		}
		parsedDigest, err := digest.Parse(pathParts[len(pathParts)-1])
		if err != nil {
			return ImageReference{}, input, fmt.Errorf("invalid digest: %q", pathParts[len(pathParts)-1])
		}
		rawRepoName = strings.TrimPrefix(pathParts[1], "/")
		manifestRef.Digest = parsedDigest
	case strings.Contains(imageURL.Path, ":"):
		// input references a tag name
		pathParts := strings.SplitN(imageURL.Path, ":", 2)
		rawRepoName = strings.TrimPrefix(pathParts[0], "/")
		manifestRef.Tag = pathParts[1]
	default:
		// input references no tag or digest - use default tag
		rawRepoName = strings.TrimPrefix(imageURL.Path, "/")
		manifestRef.Tag = "latest"
	}

	repoName, ok := CheckRepositoryName(rawRepoName).Unpack()
	if !ok {
		return ImageReference{}, input, fmt.Errorf("invalid repository name: %q", rawRepoName)
	}

	if hadNoHostName {
		// on the default registry, single-word repo names like "alpine" are
		// actually shorthands for "library/alpine" etc.
		if !strings.Contains(string(repoName), "/") {
			repoName = "library/" + repoName
		}
	}

	return ImageReference{
		Host:      imageURL.Host,
		RepoName:  repoName,
		Reference: manifestRef,
	}, input, nil
}

func looksLikeHostName(host string) bool {
	if strings.Contains(host, ":") {
		// looks like "host:port"
		return true
	}
	return strings.Contains(host, ".") || host == "localhost"
}
