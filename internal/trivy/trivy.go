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

	"github.com/sapcc/keppel/internal/models"
)

// see https://github.com/aquasecurity/trivy/blob/main/pkg/flag/remote_flags.go#L11
const (
	TokenHeader       = "Trivy-Token"
	KeppelTokenHeader = "Keppel-Token"
)

// Config contains credentials for talking to a Trivy server through a
// trivy-proxy deployment.
type Config struct {
	URL   url.URL
	Token string
}

// ScanManifest queries the Trivy server for a report on the given manifest.
func (tc *Config) ScanManifest(ctx context.Context, keppelToken string, manifestRef models.ImageReference, format string) ([]byte, error) {
	requestURL := tc.URL
	requestURL.Path = "/trivy"
	requestURL.RawQuery = url.Values{
		"image":  {manifestRef.String()},
		"format": {format},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), http.NoBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set(TokenHeader, tc.Token)
	req.Header.Set(KeppelTokenHeader, keppelToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trivy proxy did not return 200: %d %s", resp.StatusCode, respBody)
	}

	return respBody, nil
}

// ScanManifest is like ScanManifestAndParse, except that the result is parsed
// instead of being returned as a bytestring.
func (tc *Config) ScanManifestAndParse(ctx context.Context, keppelToken string, manifestRef models.ImageReference, format string) (VulnerabilityReport, error) {
	report, err := tc.ScanManifest(ctx, keppelToken, manifestRef, format)
	if err != nil {
		return VulnerabilityReport{}, err
	}

	var parsedReport VulnerabilityReport
	err = json.Unmarshal(report, &parsedReport)
	return parsedReport, err
}
