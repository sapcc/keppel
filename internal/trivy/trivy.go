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

package trivy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// see https://github.com/aquasecurity/trivy/blob/main/pkg/flag/remote_flags.go#L11
const (
	TokenHeader       = "Trivy-Token"
	KeppelTokenHeader = "Keppel-Token"
)

type Config struct {
	URL   url.URL
	Token string
}

type securityResponse struct {
	Results []struct {
		Vulnerabilities []struct {
			Severity string `json:"Severity"`
		} `json:"Vulnerabilities"`
	} `json:"Results"`
}

func (tc *Config) ScanManifest(manifestRefString, keppelToken string) (trivyReport securityResponse, returnedError error) {
	//we don't allow Trivy to take more than 10 minutes on a single image (which is already an
	//insanely generous timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	requestURL := tc.URL
	requestURL.Path = "/trivy"
	requestURL.RawQuery = url.Values{"image": {manifestRefString}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), http.NoBody)
	if err != nil {
		return securityResponse{}, err
	}

	req.Header.Set(TokenHeader, tc.Token)
	req.Header.Set(KeppelTokenHeader, keppelToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return securityResponse{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return securityResponse{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return securityResponse{}, fmt.Errorf("trivy proxy did not return 200: %d %s", resp.StatusCode, respBody)
	}

	err = json.Unmarshal(respBody, &trivyReport)
	return trivyReport, err
}
