// SPDX-FileCopyrightText: 2018 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package parse

import (
	"testing"

	"github.com/majewsky/schwift/v2"
	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/assert"
)

var (
	storageID      string            = "bd1df5ffd83b94f365adc7b9011e2079856cd4aa401ee19b6cdfcffcecad7a61"
	manifestDigest digest.Digest     = digest.Digest("sha256:1d6f90850896f753a6c4c5d8edc7086f0290ce90a34d92439c30d1257f44979f")
	repoName       string            = "foo-repository"
	chunkNumber    uint32            = 3420741
	format         string            = "json"
	container      schwift.Container = schwift.Container{}
)

func TestParseBlobObject(t *testing.T) {
	obj := BlobObject(&container, storageID)
	parsedStorageID := ParseBlobObjectName(obj.Name())
	assert.DeepEqual(t, "storageID", parsedStorageID, storageID)
}

func TestParseChunkObject(t *testing.T) {
	obj := ChunkObject(&container, storageID, chunkNumber)
	parsedStorageID, parsedChunkNumber, err := ParseChunkObjectName(obj.Name())
	if err != nil {
		t.Errorf("expected to parse chunk object name %s, but got error: %s", obj.Name(), err.Error())
	}
	assert.DeepEqual(t, "storageID", parsedStorageID, storageID)
	assert.DeepEqual(t, "chunkNumber", parsedChunkNumber, chunkNumber)
}

func TestParseManifestObject(t *testing.T) {
	obj := ManifestObject(&container, repoName, manifestDigest)
	parsedRepoName, parsedManifestDigest, err := ParseManifestObjectName(obj.Name())
	if err != nil {
		t.Errorf("expected to parse manifest object name %s, but got error: %s", obj.Name(), err.Error())
	}
	assert.DeepEqual(t, "repoName", parsedRepoName, repoName)
	assert.DeepEqual(t, "manifestDigest", parsedManifestDigest, manifestDigest)
}

func TestParseTrivyReportObject(t *testing.T) {
	obj := TrivyReportObject(&container, repoName, manifestDigest, format)
	parsedRepoName, parsedManifestDigest, parsedFormat, err := ParseTrivyReportObjectName(obj.Name())
	if err != nil {
		t.Errorf("expected to parse trivy report object name %s, but got error: %s", obj.Name(), err.Error())
	}
	assert.DeepEqual(t, "repoName", parsedRepoName, repoName)
	assert.DeepEqual(t, "manifestDigest", parsedManifestDigest, manifestDigest)
	assert.DeepEqual(t, "format", parsedFormat, format)
}
