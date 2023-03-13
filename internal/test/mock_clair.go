/*******************************************************************************
*
* Copyright 2021 SAP SE
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

package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sapcc/keppel/internal/clair"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"
)

// ClairDouble acts as a test double for a Clair API.
type ClairDouble struct {
	T *testing.T
	//key = manifest digest, value = path to JSON fixture file containing `clair.Manifest` for this image
	IndexFixtures     map[string]string
	WasIndexSubmitted map[string]bool
	//key = manifest digest, value = path to JSON fixture file containing `clair.IndexReport` for this image
	IndexReportFixtures map[string]string
	IndexDeleteCounter  int
	//key = manifest digest, value = path to JSON fixture file containing `clair.VulnerabilityReport` for this image
	ReportFixtures map[string]string
	IndexState     string
}

const IndexStateHash = "aae368a064d7c5a433d0bf2c4f5554cc"

// NewClairDouble creates a ClairDouble.
func NewClairDouble() *ClairDouble {
	return &ClairDouble{
		IndexFixtures:       make(map[string]string),
		IndexReportFixtures: make(map[string]string),
		IndexState:          IndexStateHash,
		WasIndexSubmitted:   make(map[string]bool),
		ReportFixtures:      make(map[string]string),
	}
}

// AddTo implements the api.API interface.
func (c *ClairDouble) AddTo(r *mux.Router) {
	r.Methods("POST").
		Path("/indexer/api/v1/index_report").
		HandlerFunc(c.postIndexReport)
	r.Methods("GET").
		Path("/indexer/api/v1/index_report/{digest}").
		HandlerFunc(c.getIndexReport)
	r.Methods("DELETE").
		Path("/indexer/api/v1/index_report/{digest}").
		HandlerFunc(c.deleteIndexReport)
	r.Methods("GET").
		Path("/indexer/api/v1/index_state").
		HandlerFunc(c.getIndexState)
	r.Methods("GET").
		Path("/matcher/api/v1/vulnerability_report/{digest}").
		HandlerFunc(c.getVulnerabilityReport)
}

func (c *ClairDouble) postIndexReport(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/indexer/api/v1/index_report")

	//get digest from request body
	var reqBody map[string]interface{}
	err := json.NewDecoder(r.Body).Decode(&reqBody)
	if respondwith.ErrorText(w, err) {
		return
	}
	digest := reqBody["hash"].(string) //nolint:errcheck

	//only accept images that we anticipated
	fixturePath := c.IndexFixtures[digest]
	if fixturePath == "" {
		http.Error(w, "unexpected digest: "+digest, http.StatusBadRequest)
		return
	}
	fixturePathAbs, _ := filepath.Abs(fixturePath) //nolint:errcheck
	actualPathAbs := fixturePathAbs + ".actual"

	//pretty-print actual request body into file
	reqBodyBytes, err := json.Marshal(reqBody)
	if respondwith.ErrorText(w, err) {
		return
	}
	var reqBodyBuf bytes.Buffer
	err = json.Indent(&reqBodyBuf, reqBodyBytes, "", "  ")
	if respondwith.ErrorText(w, err) {
		return
	}
	reqBodyBuf.WriteByte('\n')
	err = os.WriteFile(actualPathAbs, reqBodyBuf.Bytes(), 0o666)
	if respondwith.ErrorText(w, err) {
		return
	}

	//only accept manifests that we anticipated
	cmd := exec.Command("diff", "-u", fixturePathAbs, actualPathAbs)
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		msg := fmt.Sprintf("manifest for %s does not match fixture at %s (see diff output above)", digest, fixturePath)
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	//minimal valid response to keep Keppel going
	c.WasIndexSubmitted[digest] = true
	state := "CheckManifest"
	if c.ReportFixtures[digest] != "" {
		state = "IndexFinished"
	}
	respondwith.JSON(w, http.StatusCreated, map[string]interface{}{
		"manifest_hash": digest,
		"state":         state,
	})
}

func (c *ClairDouble) getIndexReport(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/indexer/api/v1/index_report/{digest}")

	digest := mux.Vars(r)["digest"]

	if !c.WasIndexSubmitted[digest] {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	indexReportFixture := c.IndexReportFixtures[digest]
	if indexReportFixture != "" {
		content, err := os.ReadFile(indexReportFixture)
		if respondwith.ErrorText(w, err) {
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(content)
		return
	}

	state := "CheckManifest"
	if c.ReportFixtures[digest] != "" {
		state = "IndexFinished"
	}
	respondwith.JSON(w, http.StatusCreated, map[string]interface{}{
		"manifest_hash": digest,
		"state":         state,
	})
}

func (c *ClairDouble) deleteIndexReport(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/indexer/api/v1/index_report/{digest}")

	digest := mux.Vars(r)["digest"]

	// only accept images that we anticipated
	if c.IndexFixtures[digest] == "" {
		http.Error(w, "unexpected digest: "+digest, http.StatusBadRequest)
		return
	}

	// only accept deletes if the index was submitted before
	if !c.WasIndexSubmitted[digest] {
		http.Error(w, "digest was not submitted before: "+digest, http.StatusBadRequest)
		return
	}

	c.WasIndexSubmitted[digest] = false
	c.IndexDeleteCounter += 1

	w.WriteHeader(http.StatusNoContent)
}

func (c *ClairDouble) getVulnerabilityReport(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/matcher/api/v1/vulnerability_report/{digest}")

	digest := mux.Vars(r)["digest"]
	fixturePath := c.ReportFixtures[digest]
	if !c.WasIndexSubmitted[digest] || digest == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	reportBytes, err := os.ReadFile(fixturePath)
	if respondwith.ErrorText(w, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, json.RawMessage(reportBytes))
}

func (c *ClairDouble) getIndexState(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/matcher/api/v1/index_state")

	respondwith.JSON(w, http.StatusOK, clair.IndexState{State: c.IndexState})
}
