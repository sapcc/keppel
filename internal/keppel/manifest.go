/*******************************************************************************
*
* Copyright 2020 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package keppel

import (
	"fmt"

	"github.com/sapcc/keppel/internal/models"

	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/types"
	imagespecs "github.com/opencontainers/image-spec/specs-go/v1"
)

// ParsedManifest is an interface that can interrogate manifests about the blobs
// and submanifests referenced therein.
type ParsedManifest interface {
	// FindImageConfigBlob returns the descriptor of the blob containing this
	// manifest's image configuration, or nil if the manifest does not have an image
	// configuration.
	FindImageConfigBlob() *types.BlobInfo
	// FindImageLayerBlobs returns the descriptors of the blobs containing this
	// manifest's image layers, or an empty list if the manifest does not have layers.
	FindImageLayerBlobs() []manifest.LayerInfo
	// BlobReferences returns all blobs referenced by this manifest.
	BlobReferences() []manifest.LayerInfo
	// ManifestReferences returns all manifests referenced by this manifest.
	ManifestReferences(pf models.PlatformFilter) []imagespecs.Descriptor
	// AcceptableAlternates returns the subset of ManifestReferences() that is
	// acceptable as alternate representations of this manifest. When a client
	// asks for this manifest, but the Accept header does not match the manifest
	// itself, the API will look for an acceptable alternate to serve instead.
	AcceptableAlternates(pf models.PlatformFilter) []imagespecs.Descriptor
}

var ManifestMediaTypes = []string{
	manifest.DockerV2ListMediaType,
	manifest.DockerV2Schema2MediaType,
	imagespecs.MediaTypeImageIndex,
	imagespecs.MediaTypeImageManifest,
}

// ParseManifest parses a manifest. It also returns a Descriptor describing the manifest itself.
func ParseManifest(mediaType string, contents []byte) (ParsedManifest, error) {
	// WARNING: Please update ManifestMediaTypes if any new are added.
	switch mediaType {
	case manifest.DockerV2ListMediaType:
		m, err := manifest.Schema2ListFromManifest(contents)
		if err != nil {
			return nil, err
		}
		return v2ManifestListAdapter{m}, nil
	case manifest.DockerV2Schema2MediaType:
		m, err := manifest.Schema2FromManifest(contents)
		if err != nil {
			return nil, err
		}
		return v2ManifestAdapter{m}, nil
	case imagespecs.MediaTypeImageIndex:
		m, err := manifest.OCI1IndexFromManifest(contents)
		if err != nil {
			return nil, err
		}
		return ociIndexAdapter{m}, nil
	case imagespecs.MediaTypeImageManifest:
		m, err := manifest.OCI1FromManifest(contents)
		if err != nil {
			return nil, err
		}
		return ociManifestAdapter{m}, nil
	default:
		return nil, fmt.Errorf("unsupported manifest media type: %q", mediaType)
	}
}

// v2ManifestListAdapter provides the ParsedManifest interface for the contained type.
type v2ManifestListAdapter struct {
	m *manifest.Schema2List
}

func (a v2ManifestListAdapter) FindImageConfigBlob() *types.BlobInfo {
	return nil
}

func (a v2ManifestListAdapter) FindImageLayerBlobs() []manifest.LayerInfo {
	return nil
}

func (a v2ManifestListAdapter) BlobReferences() []manifest.LayerInfo {
	return nil
}

func (a v2ManifestListAdapter) ManifestReferences(pf models.PlatformFilter) []imagespecs.Descriptor {
	result := make([]imagespecs.Descriptor, 0, len(a.m.Manifests))
	for _, m := range a.m.Manifests {
		platform := imagespecs.Platform{
			Architecture: m.Platform.Architecture,
			OS:           m.Platform.OS,
			OSVersion:    m.Platform.OSVersion,
			OSFeatures:   m.Platform.OSFeatures,
			Variant:      m.Platform.Variant,
		}
		if pf.Includes(platform) {
			descriptor := imagespecs.Descriptor{
				MediaType: m.MediaType,
				Digest:    m.Digest,
				Size:      m.Size,
				URLs:      m.URLs,
				Platform:  &platform,
			}
			result = append(result, descriptor)
		}
	}
	return result
}

func (a v2ManifestListAdapter) AcceptableAlternates(pf models.PlatformFilter) []imagespecs.Descriptor {
	var result []imagespecs.Descriptor
	for _, m := range a.ManifestReferences(pf) {
		// If we have an application/vnd.docker.distribution.manifest.list.v2+json manifest, but the
		// client only accepts application/vnd.docker.distribution.manifest.v2+json, in order to stay
		// compatible with the reference implementation of Docker Hub, we serve this case by recursing
		// into the image list and returning the linux/amd64 manifest to the client.
		//
		// This case is relevant for the support of tagged multi-arch images in `docker pull`.
		if a.m.MediaType == manifest.DockerV2ListMediaType && m.MediaType == manifest.DockerV2Schema2MediaType {
			if m.Platform.OS == "linux" && m.Platform.Architecture == "amd64" {
				result = append(result, m)
			}
		}
	}
	return result
}

// v2ManifestAdapter provides the ParsedManifest interface for the contained type.
type v2ManifestAdapter struct {
	m *manifest.Schema2
}

func (a v2ManifestAdapter) FindImageConfigBlob() *types.BlobInfo {
	config := a.m.ConfigInfo()
	return &config
}

func (a v2ManifestAdapter) FindImageLayerBlobs() []manifest.LayerInfo {
	return a.m.LayerInfos()
}

func (a v2ManifestAdapter) BlobReferences() []manifest.LayerInfo {
	references := []manifest.LayerInfo{{BlobInfo: a.m.ConfigInfo()}}
	return append(references, a.m.LayerInfos()...)
}

func (a v2ManifestAdapter) ManifestReferences(pf models.PlatformFilter) []imagespecs.Descriptor {
	return nil
}

func (a v2ManifestAdapter) AcceptableAlternates(pf models.PlatformFilter) []imagespecs.Descriptor {
	return nil
}

// v2ManifestListAdapter provides the ParsedManifest interface for the contained type.
type ociIndexAdapter struct {
	m *manifest.OCI1Index
}

func (a ociIndexAdapter) FindImageConfigBlob() *types.BlobInfo {
	return nil
}

func (a ociIndexAdapter) FindImageLayerBlobs() []manifest.LayerInfo {
	return nil
}

func (a ociIndexAdapter) BlobReferences() []manifest.LayerInfo {
	return nil
}

func (a ociIndexAdapter) ManifestReferences(pf models.PlatformFilter) []imagespecs.Descriptor {
	result := make([]imagespecs.Descriptor, 0, len(a.m.Manifests))
	for _, m := range a.m.Manifests {
		if m.Platform == nil || pf.Includes(*m.Platform) {
			result = append(result, m)
		}
	}
	return result
}

func (a ociIndexAdapter) AcceptableAlternates(pf models.PlatformFilter) []imagespecs.Descriptor {
	return nil
}

// ociManifestAdapter provides the ParsedManifest interface for the contained type.
type ociManifestAdapter struct {
	m *manifest.OCI1
}

func (a ociManifestAdapter) FindImageConfigBlob() *types.BlobInfo {
	// Standard OCI images have this specific MediaType for their config blob, and
	// this is the format that we can inspect.
	if a.m.Config.MediaType == imagespecs.MediaTypeImageConfig {
		config := a.m.ConfigInfo()
		return &config
	}
	// ORAS images have application-specific MediaTypes that we do not know how to
	// inspect (e.g. `application/vnd.aquasec.trivy.config.v1+json` for Trivy
	// vulnerability DBs). We have to ignore these since we cannot parse them.
	return nil
}

func (a ociManifestAdapter) FindImageLayerBlobs() []manifest.LayerInfo {
	return a.m.LayerInfos()
}

func (a ociManifestAdapter) BlobReferences() []manifest.LayerInfo {
	references := []manifest.LayerInfo{{BlobInfo: a.m.ConfigInfo()}}
	return append(references, a.m.LayerInfos()...)
}

func (a ociManifestAdapter) ManifestReferences(pf models.PlatformFilter) []imagespecs.Descriptor {
	return nil
}

func (a ociManifestAdapter) AcceptableAlternates(pf models.PlatformFilter) []imagespecs.Descriptor {
	return nil
}
