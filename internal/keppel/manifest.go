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

	"github.com/docker/distribution"

	//distribution.UnmarshalManifest() relies on the following packages
	//registering their manifest schemas.
	"github.com/docker/distribution/manifest/manifestlist"
	"github.com/docker/distribution/manifest/ocischema"
	"github.com/docker/distribution/manifest/schema2"
)

//NOTE: We don't enable github.com/docker/distribution/manifest/schema1
//anymore since it's legacy anyway and the implementation is a lot simpler
//when we don't have to rewrite manifests between schema1 and schema2.

//IsManifestMediaType returns whether the given media type is for a manifest.
func IsManifestMediaType(mediaType string) bool {
	for _, mt := range distribution.ManifestMediaTypes() {
		if mt == mediaType {
			return true
		}
	}
	return false
}

//ParsedManifest is an interface that can interrogate manifests about the blobs
//and submanifests referenced therein.
type ParsedManifest interface {
	//FindImageConfigBlob returns the descriptor of the blob containing this
	//manifest's image configuration, or nil if the manifest does not have an image
	//configuration.
	FindImageConfigBlob() *distribution.Descriptor
	//FindImageLayerBlobs returns the descriptors of the blobs containing this
	//manifest's image layers, or an empty list if the manifest does not have layers.
	FindImageLayerBlobs() []distribution.Descriptor
	//BlobReferences returns all blobs referenced by this manifest.
	BlobReferences() []distribution.Descriptor
	//ManifestReferences returns all manifests referenced by this manifest.
	//This takes an account as argument because the `account.PlatformFilter` may
	//need to be considered.
	ManifestReferences(account Account) []manifestlist.ManifestDescriptor
}

//ParseManifest parses a manifest. It also returns a Descriptor describing the manifest itself.
func ParseManifest(mediaType string, contents []byte) (ParsedManifest, distribution.Descriptor, error) {
	m, desc, err := distribution.UnmarshalManifest(mediaType, contents)
	if err != nil {
		return nil, distribution.Descriptor{}, err
	}
	switch m := m.(type) {
	case *schema2.DeserializedManifest:
		return v2ManifestAdapter{m}, desc, nil
	case *ocischema.DeserializedManifest:
		return ociManifestAdapter{m}, desc, nil
	case *manifestlist.DeserializedManifestList:
		return listManifestAdapter{m}, desc, nil
	default:
		panic(fmt.Sprintf("unexpected manifest type: %T", m))
	}
}

//v2ManifestAdapter provides the ParsedManifest interface for the contained type.
type v2ManifestAdapter struct {
	m *schema2.DeserializedManifest
}

func (a v2ManifestAdapter) FindImageConfigBlob() *distribution.Descriptor {
	return &a.m.Config
}

func (a v2ManifestAdapter) FindImageLayerBlobs() []distribution.Descriptor {
	return a.m.Layers
}

func (a v2ManifestAdapter) BlobReferences() []distribution.Descriptor {
	return a.m.References()
}

func (a v2ManifestAdapter) ManifestReferences(account Account) []manifestlist.ManifestDescriptor {
	return nil
}

//ociManifestAdapter provides the ParsedManifest interface for the contained type.
type ociManifestAdapter struct {
	m *ocischema.DeserializedManifest
}

func (a ociManifestAdapter) FindImageConfigBlob() *distribution.Descriptor {
	return &a.m.Config
}

func (a ociManifestAdapter) FindImageLayerBlobs() []distribution.Descriptor {
	return a.m.Layers
}

func (a ociManifestAdapter) BlobReferences() []distribution.Descriptor {
	return a.m.References()
}

func (a ociManifestAdapter) ManifestReferences(account Account) []manifestlist.ManifestDescriptor {
	return nil
}

//listManifestAdapter provides the ParsedManifest interface for the contained type.
type listManifestAdapter struct {
	m *manifestlist.DeserializedManifestList
}

func (a listManifestAdapter) FindImageConfigBlob() *distribution.Descriptor {
	return nil
}

func (a listManifestAdapter) FindImageLayerBlobs() []distribution.Descriptor {
	return nil
}

func (a listManifestAdapter) BlobReferences() []distribution.Descriptor {
	return nil
}

func (a listManifestAdapter) ManifestReferences(account Account) []manifestlist.ManifestDescriptor {
	result := make([]manifestlist.ManifestDescriptor, 0, len(a.m.Manifests))
	for _, m := range a.m.Manifests {
		if account.PlatformFilter.Includes(m.Platform) {
			result = append(result, m)
		}
	}
	return result
}
