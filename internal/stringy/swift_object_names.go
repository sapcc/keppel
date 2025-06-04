// SPDX-FileCopyrightText: 2018 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package stringy

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/opencontainers/go-digest"
)

// BlobObjectName builds the name under which a blob with the provided storage ID will be stored in a Swift container.
func BlobObjectName(storageID string) string {
	return fmt.Sprintf("_blobs/%s/%s/%s", storageID[0:2], storageID[2:4], storageID[4:])
}

// ChunkObjectName builds the name under which a chunk with the provided storage ID and chunk number will be stored in a Swift container.
func ChunkObjectName(storageID string, chunkNumber uint32) string {
	//NOTE: uint32 numbers never have more than 10 digits
	return fmt.Sprintf("_chunks/%s/%s/%s/%010d", storageID[0:2], storageID[2:4], storageID[4:], chunkNumber)
}

// ManifestObjectName builds the name under which a manifest with the provided repository name and digest will be stored in a Swift container.
func ManifestObjectName(repoName string, manifestDigest digest.Digest) string {
	return fmt.Sprintf("%s/_manifests/%s", repoName, manifestDigest)
}

// TrivyReportObjectName builds the name under which a trivy report with the provided repository name, digest and format will be stored in a Swift container.
func TrivyReportObjectName(repoName string, manifestDigest digest.Digest, format string) string {
	return fmt.Sprintf("%s/_trivyreports/%s/%s", repoName, manifestDigest, format)
}

var (
	// These regexes are used to reconstruct the storage ID from a blob's or chunk's object name.
	// It's kinda the reverse of func BlobObjectName() and ChunkObjectName().
	blobObjectNameRx  = regexp.MustCompile(`^_blobs/([^/]{2})/([^/]{2})/([^/]+)$`)
	chunkObjectNameRx = regexp.MustCompile(`^_chunks/([^/]{2})/([^/]{2})/([^/]+)/([0-9]+)$`)
	// This regex recovers the repo name and manifest digest from a manifest's object name.
	// It's kinda the reverse of func ManifestObjectName().
	manifestObjectNameRx = regexp.MustCompile(`^(.+)/_manifests/([^/]+)$`)
	// This regex recovers the repo name, manifest digest and format from a Trivy report's object name.
	// It's kinda the reverse of func TrivyReportObjectName().
	trivyReportObjectNameRx = regexp.MustCompile(`^(.+)/_trivyreports/([^/]+)/([^/]+)$`)
)

// ParseBlobObjectName checks if the provided name of a Swift object refers to a blob object.
// If so, the storage ID is decoded from the name. Otherwise, an empty string is returned.
func ParseBlobObjectName(name string) (storageID string) {
	match := blobObjectNameRx.FindStringSubmatch(name)
	if match == nil {
		return ""
	}
	return match[1] + match[2] + match[3]
}

// ParseChunkObjectName checks if the provided name of a Swift object refers to a chunk object.
// If so, the storage ID and chunk number are decoded from the name. Otherwise, zero values are returned.
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

// ParseManifestObjectName checks if the provided name of a Swift object refers to a manifest object.
// If so, the repository name and digest are decoded from the name. Otherwise, zero values are returned.
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

// ParseTrivyReportObjectName checks if the provided name of a Swift object refers to a trivy report object.
// If so, the repository name, digest and format are decoded from the name. Otherwise, zero values are returned.
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
