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

package models

import (
	"time"

	"github.com/opencontainers/go-digest"
)

// Manifest contains a record from the `manifests` table.
type Manifest struct {
	RepositoryID           int64         `db:"repo_id"`
	Digest                 digest.Digest `db:"digest"`
	MediaType              string        `db:"media_type"`
	SizeBytes              uint64        `db:"size_bytes"`
	PushedAt               time.Time     `db:"pushed_at"`
	NextValidationAt       time.Time     `db:"next_validation_at"` // see tasks.ManifestValidationJob
	ValidationErrorMessage string        `db:"validation_error_message"`
	LastPulledAt           *time.Time    `db:"last_pulled_at"`
	// LabelsJSON contains a JSON string of a map[string]string, or an empty string.
	LabelsJSON string `db:"labels_json"`
	// GCStatusJSON contains a keppel.GCStatus serialized into JSON, or an empty
	// string if GC has not seen this manifest yet.
	GCStatusJSON      string         `db:"gc_status_json"`
	MinLayerCreatedAt *time.Time     `db:"min_layer_created_at"`
	MaxLayerCreatedAt *time.Time     `db:"max_layer_created_at"`
	ArtifactType      string         `db:"artifact_type"`
	SubjectDigest     *digest.Digest `db:"subject_digest"`
}

const (
	// ManifestValidationInterval is how often each manifest will be validated by ManifestValidationJob.
	// This is here instead of near the job because package processor also needs to know it.
	ManifestValidationInterval = 24 * time.Hour
	// ManifestValidationAfterErrorInterval is how quickly ManifestValidationJob will retry a failed manifest validation.
	ManifestValidationAfterErrorInterval = 10 * time.Minute
)

// Tag contains a record from the `tags` table.
type Tag struct {
	RepositoryID int64         `db:"repo_id"`
	Name         string        `db:"name"`
	Digest       digest.Digest `db:"digest"`
	PushedAt     time.Time     `db:"pushed_at"`
	LastPulledAt *time.Time    `db:"last_pulled_at"`
}

// ManifestContent contains a record from the `manifest_contents` table.
type ManifestContent struct {
	RepositoryID int64  `db:"repo_id"`
	Digest       string `db:"digest"`
	Content      []byte `db:"content"`
}
