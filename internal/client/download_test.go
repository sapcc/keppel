// Copyright 2023 SAP SE
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package client

import (
	"sort"
	"testing"

	"github.com/docker/distribution"
	"github.com/sapcc/go-bits/assert"
)

func TestManifestMediaTypes(t *testing.T) {
	// in case we update docker/distribution and new manifest media types are added, we want to know that
	mediaTypes := distribution.ManifestMediaTypes()
	sort.Strings(mediaTypes)
	assert.DeepEqual(t, "mediaTypes", mediaTypes, []string{
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.oci.image.manifest.v1+json",
	})
}
