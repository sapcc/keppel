// SPDX-FileCopyrightText: 2018 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package parse

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/majewsky/schwift/v2"
	"github.com/opencontainers/go-digest"
)

func BlobObject(c *schwift.Container, storageID string) *schwift.Object {
	return c.Object(fmt.Sprintf("_blobs/%s/%s/%s", storageID[0:2], storageID[2:4], storageID[4:]))
}

func ChunkObject(c *schwift.Container, storageID string, chunkNumber uint32) *schwift.Object {
	//NOTE: uint32 numbers never have more than 10 digits
	return c.Object(fmt.Sprintf("_chunks/%s/%s/%s/%010d", storageID[0:2], storageID[2:4], storageID[4:], chunkNumber))
}

func ManifestObject(c *schwift.Container, repoName string, manifestDigest digest.Digest) *schwift.Object {
	return c.Object(fmt.Sprintf("%s/_manifests/%s", repoName, manifestDigest))
}

func TrivyReportObject(c *schwift.Container, repoName string, manifestDigest digest.Digest, format string) *schwift.Object {
	return c.Object(fmt.Sprintf("%s/_trivyreports/%s/%s", repoName, manifestDigest, format))
}

var (
	// These regexes are used to reconstruct the storage ID from a blob's or chunk's object name.
	// It's kinda the reverse of func blobObject().
	blobObjectNameRx  = regexp.MustCompile(`^_blobs/([^/]{2})/([^/]{2})/([^/]+)$`)
	chunkObjectNameRx = regexp.MustCompile(`^_chunks/([^/]{2})/([^/]{2})/([^/]+)/([0-9]+)$`)
	// This regex recovers the repo name and manifest digest from a manifest's object name.
	// It's kinda the reverse of func manifestObject().
	manifestObjectNameRx = regexp.MustCompile(`^(.+)/_manifests/([^/]+)$`)
	// This regex recovers the repo name, manifest digest and format from a Trivy report's object name.
	// It's kinda the reverse of func trivyReportObject().
	trivyReportObjectNameRx = regexp.MustCompile(`^(.+)/_trivyreports/([^/]+)/([^/]+)$`)
)

func ParseBlobObjectName(name string) (storageID string) {
	match := blobObjectNameRx.FindStringSubmatch(name)
	if match == nil {
		return ""
	}
	return match[1] + match[2] + match[3]
}

func ParseChunkObjectName(name string) (storageID string, chunkNumber uint32, err error) {
	match := chunkObjectNameRx.FindStringSubmatch(name)
	if match == nil {
		return "", 0, nil
	}
	storageID = match[1] + match[2] + match[3]
	chunkNumber64, err := strconv.ParseUint(match[4], 10, 32)
	if err != nil {
		return "", 0, fmt.Errorf("while parsing chunk object name %q: %w", name, err)
	}
	return storageID, uint32(chunkNumber64), nil
}

func ParseManifestObjectName(name string) (repoName string, manifestDigest digest.Digest, err error) {
	match := manifestObjectNameRx.FindStringSubmatch(name)
	if match == nil {
		return "", "", nil
	}
	manifestDigest, err = digest.Parse(match[2])
	if err != nil {
		return "", "", fmt.Errorf("while parsing manifest object name %q: %w", name, err)
	}
	return match[1], manifestDigest, nil
}

func ParseTrivyReportObjectName(name string) (repoName string, manifestDigest digest.Digest, format string, err error) {
	match := trivyReportObjectNameRx.FindStringSubmatch(name)
	if match == nil {
		return "", "", "", nil
	}
	manifestDigest, err = digest.Parse(match[2])
	if err != nil {
		return "", "", "", fmt.Errorf("while parsing Trivy report object name %q: %w", name, err)
	}
	return match[1], manifestDigest, match[3], nil
}
