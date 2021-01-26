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
)

//Manifest is the representation of an image manifest that gets submitted to
//Clair for indexing.
type Manifest struct {
	Digest string  `json:"hash"`
	Layers []Layer `json:"layers"`
}

//Layer appears in type Manifest.
type Layer struct {
	Digest  string      `json:"hash"`
	URL     string      `json:"uri"`
	Headers http.Header `json:"headers"`
}

//ManifestState is returned by CheckManifestState.
type ManifestState struct {
	IsIndexed bool
	IsErrored bool
}

type indexReport struct {
	Digest string `json:"manifest_hash"`
	State  string `json:"state"`
	//there are more fields, but we are not interested in them
}

func (r indexReport) IntoManifestState() ManifestState {
	return ManifestState{
		IsIndexed: r.State == "IndexFinished",
		IsErrored: r.State == "IndexError",
	}
}

//CheckManifestState submits the manifest to clair for indexing if not done
//yet, and checks if the indexing has finished.
func (c *Client) CheckManifestState(m Manifest) (ManifestState, error) {
	req, err := http.NewRequest("GET", c.requestURL("indexer", "api", "v1", "index_report", m.Digest), nil)
	if err != nil {
		return ManifestState{}, err
	}
	var result indexReport
	err = c.doRequest(req, &result)
	if err != nil && strings.Contains(err.Error(), "got 404 response") {
		result, err = c.submitManifest(m)
	}
	return result.IntoManifestState(), err
}

func (c *Client) submitManifest(m Manifest) (indexReport, error) {
	jsonBytes, err := json.Marshal(m)
	if err != nil {
		return indexReport{}, err
	}

	req, err := http.NewRequest(
		"POST",
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
