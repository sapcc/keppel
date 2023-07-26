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
	"strings"

	"github.com/aquasecurity/trivy/pkg/types"
	prettyText "github.com/jedib0t/go-pretty/v6/text"

	"github.com/sapcc/keppel/internal/models"
)

// MapToTrivySeverity maps Trivy severity levels to ours
// see https://github.com/aquasecurity/trivy/blob/main/pkg/report/table/misconfig.go#L19-L24
var MapToTrivySeverity = map[string]VulnerabilityStatus{
	"UNKNOWN":  UnknownSeverity,
	"LOW":      LowSeverity,
	"MEDIUM":   MediumSeverity,
	"HIGH":     HighSeverity,
	"CRITICAL": CriticalSeverity,
}

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

// ReportPayload contains a report that was returned by Trivy (and potentially
// enhanced by Keppel).
type ReportPayload struct {
	Format   string
	Contents []byte
}

// ScanManifest queries the Trivy server for a report on the given manifest.
func (tc *Config) ScanManifest(ctx context.Context, keppelToken string, manifestRef models.ImageReference, format string) (ReportPayload, error) {
	requestURL := tc.URL
	requestURL.Path = "/trivy"
	requestURL.RawQuery = url.Values{
		"image":  {manifestRef.String()},
		"format": {format},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), http.NoBody)
	if err != nil {
		return ReportPayload{}, err
	}

	req.Header.Set(TokenHeader, tc.Token)
	req.Header.Set(KeppelTokenHeader, keppelToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ReportPayload{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ReportPayload{}, err
	}
	if resp.StatusCode != http.StatusOK {
		// from inner to outer: cast to string, remove extra new lines, remove color escape codes, replace multiple consecutive spaces with one
		respCleaned := strings.Join(strings.Fields(prettyText.StripEscape(strings.TrimSpace(string(respBody)))), " ")
		return ReportPayload{}, fmt.Errorf("trivy proxy did not return 200: %d %s", resp.StatusCode, respCleaned)
	}

	return ReportPayload{Format: format, Contents: respBody}, nil
}

// ScanManifest is like ScanManifestAndParse, except that the result is parsed
// instead of being returned as a bytestring. The report format "json" is
// implied in order to match the return type.
func (tc *Config) ScanManifestAndParse(ctx context.Context, keppelToken string, manifestRef models.ImageReference) (types.Report, error) {
	report, err := tc.ScanManifest(ctx, keppelToken, manifestRef, "json")
	if err != nil {
		return types.Report{}, err
	}

	var parsedReport types.Report
	err = json.Unmarshal(report.Contents, &parsedReport)
	return parsedReport, err
}

// FixIsReleased returns whether v.FixedVersion is non-empty. (This particular
// method name reads better in some situations than `v.FixedVersion != ""`.)
func FixIsReleased(v types.DetectedVulnerability) bool {
	return v.FixedVersion != ""
}
