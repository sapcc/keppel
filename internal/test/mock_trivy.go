// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"

	"github.com/sapcc/keppel/internal/client"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/trivy"
)

// TrivyDouble acts as a test double for a Trivy API.
type TrivyDouble struct {
	T              *testing.T
	ReportError    map[models.ImageReference]bool
	ReportFixtures map[models.ImageReference]string
}

// NewTrivyDouble creates a TrivyDouble.
func NewTrivyDouble() *TrivyDouble {
	return &TrivyDouble{
		ReportError:    make(map[models.ImageReference]bool),
		ReportFixtures: make(map[models.ImageReference]string),
	}
}

// AddTo implements the api.API interface.
func (t *TrivyDouble) AddTo(r *mux.Router) {
	r.Methods("GET").
		Path("/trivy").
		HandlerFunc(t.mockRunTrivy)
}

func (t *TrivyDouble) mockRunTrivy(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/trivy")

	imageRef, _, err := models.ParseImageReference(r.URL.Query().Get("image"))
	if err != nil {
		http.Error(w, "can't parse image reference: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}

	// simulate manifest download by Trivy (this implicitly verifies that pulls
	// using the Trivy token do not count towards last_pulled_at)
	c := &client.RepoClient{
		Host:     imageRef.Host,
		RepoName: imageRef.RepoName,
	}
	c.SetToken(r.Header[http.CanonicalHeaderKey(trivy.KeppelTokenHeader)][0])
	_, _, err = c.DownloadManifest(r.Context(), imageRef.Reference, &client.DownloadManifestOpts{})
	if respondwith.ErrorText(w, err) {
		return
	}

	if t.ReportError[imageRef] {
		http.Error(w, "simulated error", http.StatusInternalServerError)
		return
	}

	fixturePath := t.ReportFixtures[imageRef]
	if fixturePath == "" {
		http.Error(w, fmt.Sprintf("fixture for image '%s' not found", imageRef), http.StatusInternalServerError)
		return
	}

	reportBytes, err := os.ReadFile(fixturePath)
	if respondwith.ErrorText(w, err) {
		return
	}

	respondwith.JSON(w, http.StatusOK, json.RawMessage(reportBytes))
}
