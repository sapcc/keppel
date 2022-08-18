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

package clair

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sapcc/go-bits/logg"
)

// Manifest is the representation of an image manifest that gets submitted to
// Clair for indexing.
type Manifest struct {
	Digest string  `json:"hash"`
	Layers []Layer `json:"layers"`
}

// Layer appears in type Manifest.
type Layer struct {
	Digest  string      `json:"hash"`
	URL     string      `json:"uri"`
	Headers http.Header `json:"headers,omitempty"`
}

// ManifestState is returned by CheckManifestState.
type ManifestState struct {
	IsIndexed    bool
	IsErrored    bool
	ErrorMessage string
}

type indexReport struct {
	Digest       string `json:"manifest_hash"`
	State        string `json:"state"`
	ErrorMessage string `json:"err"`
	//there are more fields, but we are not interested in them
}

func (r indexReport) IntoManifestState() ManifestState {
	return ManifestState{
		IsIndexed:    r.State == "IndexFinished",
		IsErrored:    r.State == "IndexError",
		ErrorMessage: r.ErrorMessage,
	}
}

// CheckManifestState submits the manifest to clair for indexing if not done
// yet, and checks if the indexing has finished. Since the manifest rendering is
// costly, it's wrapped in a callback that this method only calls when needed.
func (c *Client) CheckManifestState(digest string, renderManifest func() (Manifest, error)) (ManifestState, error) {
	req, err := http.NewRequest(http.MethodGet, c.requestURL("indexer", "api", "v1", "index_report", digest), http.NoBody)
	if err != nil {
		return ManifestState{}, err
	}
	var result indexReport
	err = c.doRequest(req, &result)
	if err != nil && strings.Contains(err.Error(), "got 404 response") {
		result, err = c.submitManifest(renderManifest)
	}
	return result.IntoManifestState(), err
}

func (c *Client) submitManifest(renderManifest func() (Manifest, error)) (indexReport, error) {
	m, err := renderManifest()
	if err != nil {
		return indexReport{}, err
	}

	//Clair does not like manifests with no contents, but those do exist (for
	//healthchecks, conformance tests, etc.), so generate a bogus indexReport for
	//those
	if len(m.Layers) == 0 {
		if c.isEmptyManifest == nil {
			c.isEmptyManifest = make(map[string]bool)
		}
		c.isEmptyManifest[m.Digest] = true //remind ourselves to also fake the VulnerabilityReport later
		return indexReport{
			Digest: m.Digest,
			State:  "IndexFinished",
		}, nil
	}

	jsonBytes, err := json.Marshal(m)
	if err != nil {
		return indexReport{}, err
	}
	logg.Debug("sending indexing request to Clair: %s", string(jsonBytes))

	req, err := http.NewRequest(
		http.MethodPost,
		c.requestURL("indexer", "api", "v1", "index_report"),
		bytes.NewReader(jsonBytes),
	)
	if err != nil {
		return indexReport{}, err
	}
	var result indexReport
	err = c.doRequest(req, &result)
	return result, err
}
