// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/opencontainers/image-spec/specs-go"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/models"
)

var getManifestBySubjectQuery = sqlext.SimplifyWhitespace(`
  SELECT * FROM manifests WHERE repo_id = $1 AND subject_digest = $2
`)

var getManifestBySubjectAndArtifactTypeQuery = sqlext.SimplifyWhitespace(`
  SELECT * FROM manifests WHERE repo_id = $1 AND subject_digest = $2 AND artifact_type = $3
`)

func (a *API) handleGetReferrers(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/v2/:account/:repo/referrers/:reference")

	account, repo, _, _ := a.checkAccountAccess(w, r, failIfRepoMissing, nil)
	if account == nil {
		return
	}

	digest := mux.Vars(r)["reference"]
	var dbManifests []models.Manifest

	filterArtifactType := r.URL.Query().Get("artifactType")
	var err error
	if filterArtifactType == "" {
		_, err = a.db.Select(&dbManifests, getManifestBySubjectQuery, repo.ID, digest)
	} else {
		_, err = a.db.Select(&dbManifests, getManifestBySubjectAndArtifactTypeQuery, repo.ID, digest, filterArtifactType)
	}
	if respondWithError(w, r, err) {
		return
	}

	// the spec expects an empty list not null!
	manifests := make([]imgspecv1.Descriptor, 0)
	for _, dbManifest := range dbManifests {
		var annotations map[string]string
		if dbManifest.AnnotationsJSON != "" {
			err = json.Unmarshal([]byte(dbManifest.AnnotationsJSON), &annotations)
			if respondWithError(w, r, err) {
				return
			}
		}

		manifest := imgspecv1.Descriptor{
			MediaType:   dbManifest.MediaType,
			Size:        int64(dbManifest.SizeBytes),
			Digest:      dbManifest.Digest,
			Annotations: annotations,
		}

		artifactType := dbManifest.ArtifactType
		if artifactType == "" {
			artifactType = dbManifest.MediaType
		}
		if artifactType != "" {
			manifest.ArtifactType = artifactType
		}

		manifests = append(manifests, manifest)
	}

	// TODO: pagination?
	if filterArtifactType != "" {
		w.Header().Set("OCI-Filters-Applied", "artifactType")
	}
	w.Header().Set("Content-Type", imgspecv1.MediaTypeImageIndex)
	_ = json.NewEncoder(w).Encode(imgspecv1.Index{ //nolint: errcheck // can't fail
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: imgspecv1.MediaTypeImageIndex,
		Manifests: manifests,
	})
}
