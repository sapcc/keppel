/*******************************************************************************
*
* Copyright 2023 SAP SE
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
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"
)

// TrivyDouble acts as a test double for a Trivy API.
type TrivyDouble struct {
	T              *testing.T
	ReportError    map[string]bool
	ReportFixtures map[string]string
}

// NewTrivyDouble creates a TrivyDouble.
func NewTrivyDouble() *TrivyDouble {
	return &TrivyDouble{
		ReportError:    make(map[string]bool),
		ReportFixtures: make(map[string]string),
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

	image := r.URL.Query().Get("image")

	if t.ReportError[image] {
		http.Error(w, "simulated error", http.StatusInternalServerError)
		return
	}

	fixturePath := t.ReportFixtures[image]
	if fixturePath == "" {
		http.Error(w, fmt.Sprintf("fixture for image '%s' not found", image), http.StatusInternalServerError)
		return
	}

	reportBytes, err := os.ReadFile(fixturePath)
	if respondwith.ErrorText(w, err) {
		return
	}

	respondwith.JSON(w, http.StatusOK, json.RawMessage(reportBytes))
}
