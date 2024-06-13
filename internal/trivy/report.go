/*******************************************************************************
*
* Copyright 2024 SAP SE
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

package trivy

import (
	"time"

	ftypes "github.com/aquasecurity/trivy/pkg/fanal/types"
	stypes "github.com/aquasecurity/trivy/pkg/module/serialize"
)

// Report is mostly the same type as type Report from github.com/aquasecurity/trivy/pkg/types,
// but we explicitly copy this type here (and replace some fields with more generic types)
// to avoid importing a bazillion transitive dependencies.
type Report struct {
	SchemaVersion int            `json:",omitempty"`
	CreatedAt     time.Time      `json:",omitempty"`
	ArtifactName  string         `json:",omitempty"`
	ArtifactType  string         `json:",omitempty"` // generic replacement for original type `artifact.Type`
	Metadata      Metadata       `json:",omitempty"` // generic replacement for original type `types.Metadata`
	Results       stypes.Results `json:",omitempty"` // compatible replacement for original type `types.Results`
}

// Metadata is a generic replacement for type Metadata from github.com/aquasecurity/trivy/pkg/types,
// see documentation on type Report for details.
type Metadata struct {
	Size int64      `json:",omitempty"`
	OS   *ftypes.OS `json:",omitempty"`

	// Container image
	ImageID     string         `json:",omitempty"`
	DiffIDs     []string       `json:",omitempty"`
	RepoTags    []string       `json:",omitempty"`
	RepoDigests []string       `json:",omitempty"`
	ImageConfig map[string]any `json:",omitempty"`
}
