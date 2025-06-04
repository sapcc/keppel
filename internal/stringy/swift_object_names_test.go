// SPDX-FileCopyrightText: 2018 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package stringy

import (
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/assert"
)

var (
	storageID         string        = "bd1df5ffd83b94f365adc7b9011e2079856cd4aa401ee19b6cdfcffcecad7a61"
	manifestDigest    digest.Digest = digest.Digest("sha256:1d6f90850896f753a6c4c5d8edc7086f0290ce90a34d92439c30d1257f44979f")
	repoName          string        = "foo-repository"
	chunkNumber       uint32        = 3420741
	format            string        = "json"
	unknownObjectName string        = "_unknown/foo/bar"
)

func TestParseBlobObject(t *testing.T) {
	parsedStorageID := ParseBlobObjectName(unknownObjectName)
	if parsedStorageID != "" {
		t.Errorf("expected an empty storage ID when parsing a non-blob object name, but got: %s", parsedStorageID)
	}

	// storage ID should remain the same after parsing in both directions
	objName := BlobObjectName(storageID)
	parsedStorageID = ParseBlobObjectName(objName)
	assert.DeepEqual(t, "storageID", parsedStorageID, storageID)
}

func TestParseChunkObject(t *testing.T) {
	parsedStorageID, parsedChunkNumber, err := ParseChunkObjectName(unknownObjectName)
	if parsedStorageID != "" {
		t.Errorf("expected an empty storage ID when parsing a non-chunk object name, but got: %s", parsedStorageID)
	}
	if parsedChunkNumber != 0 {
		t.Errorf("expected a zero value for chunk number when parsing a non-chunk object name, but got: %d", parsedChunkNumber)
	}
	if err != nil {
		t.Errorf("expected to parse unknown object name %s, but got error: %s", unknownObjectName, err.Error())
	}

	_, _, err = ParseChunkObjectName("_chunks/aa/bb/cccc/999999999999999")
	if err == nil {
		t.Error("expected an error when trying to parse an invalid chunk number but got none")
	}

	// storage ID and chunk number should remain the same after parsing in both directions
	objName := ChunkObjectName(storageID, chunkNumber)
	parsedStorageID, parsedChunkNumber, err = ParseChunkObjectName(objName)
	if err != nil {
		t.Errorf("expected to parse chunk object name %s, but got error: %s", objName, err.Error())
	}
	assert.DeepEqual(t, "storageID", parsedStorageID, storageID)
	assert.DeepEqual(t, "chunkNumber", parsedChunkNumber, chunkNumber)
}

func TestParseManifestObject(t *testing.T) {
	parsedRepoName, parsedManifestDigest, err := ParseManifestObjectName(unknownObjectName)
	if parsedRepoName != "" {
		t.Errorf("expected an empty repository name when parsing a non-manifest object name, but got: %s", parsedRepoName)
	}
	if parsedManifestDigest != "" {
		t.Errorf("expected an empty digest when parsing a non-manifest object name, but got: %s", parsedManifestDigest)
	}
	if err != nil {
		t.Errorf("expected to parse unknown object name %s, but got error: %s", unknownObjectName, err.Error())
	}

	_, _, err = ParseManifestObjectName("foo-repo/_manifests/notAValidDigest")
	if err == nil {
		t.Error("expected an error when trying to parse an invalid digest but got none")
	}

	// repository name and digest should remain the same after parsing in both directions
	objName := ManifestObjectName(repoName, manifestDigest)
	parsedRepoName, parsedManifestDigest, err = ParseManifestObjectName(objName)
	if err != nil {
		t.Errorf("expected to parse manifest object name %s, but got error: %s", objName, err.Error())
	}
	assert.DeepEqual(t, "repoName", parsedRepoName, repoName)
	assert.DeepEqual(t, "manifestDigest", parsedManifestDigest, manifestDigest)
}

func TestParseTrivyReportObject(t *testing.T) {
	parsedRepoName, parsedManifestDigest, parsedFormat, err := ParseTrivyReportObjectName(unknownObjectName)
	if parsedRepoName != "" {
		t.Errorf("expected an empty repository name when parsing a non-trivy report object name, but got: %s", parsedRepoName)
	}
	if parsedManifestDigest != "" {
		t.Errorf("expected an empty digest when parsing a non-trivy report object name, but got: %s", parsedManifestDigest)
	}
	if parsedFormat != "" {
		t.Errorf("expected an empty format when parsing a non-trivy report object name, but got: %s", parsedFormat)
	}
	if err != nil {
		t.Errorf("expected to parse unknown object name %s, but got error: %s", unknownObjectName, err.Error())
	}

	_, _, _, err = ParseTrivyReportObjectName("foo-repo/_trivyreports/notAValidDigest/json")
	if err == nil {
		t.Error("expected an error when trying to parse an invalid digest but got none")
	}

	// repository name, digest and format should remain the same after parsing in both directions
	objName := TrivyReportObjectName(repoName, manifestDigest, format)
	parsedRepoName, parsedManifestDigest, parsedFormat, err = ParseTrivyReportObjectName(objName)
	if err != nil {
		t.Errorf("expected to parse trivy report object name %s, but got error: %s", objName, err.Error())
	}
	assert.DeepEqual(t, "repoName", parsedRepoName, repoName)
	assert.DeepEqual(t, "manifestDigest", parsedManifestDigest, manifestDigest)
	assert.DeepEqual(t, "format", parsedFormat, format)
}
