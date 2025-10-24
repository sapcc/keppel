// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package trivy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/sapcc/keppel/internal/models"
)

// MapToTrivySeverity maps Trivy severity levels to ours
// see https://github.com/aquasecurity/trivy/blob/main/pkg/report/table/misconfig.go#L19-L24
var MapToTrivySeverity = map[string]models.VulnerabilityStatus{
	"UNKNOWN":  models.UnknownSeverity,
	"LOW":      models.LowSeverity,
	"MEDIUM":   models.MediumSeverity,
	"HIGH":     models.HighSeverity,
	"CRITICAL": models.CriticalSeverity,
}

// see https://github.com/aquasecurity/trivy/blob/main/pkg/flag/remote_flags.go#L11
const (
	TokenHeader       = "Trivy-Token"
	KeppelTokenHeader = "Keppel-Token"
)

// Config contains credentials for talking to a Trivy server through a
// trivy-proxy deployment.
type Config struct {
	AdditionalPullableRepos []string
	Token                   string
	URL                     url.URL
}

// ReportPayload contains a report that was returned by Trivy (and potentially
// enhanced by Keppel).
type ReportPayload struct {
	Format   string
	Contents io.ReadCloser
}

// ScanManifest queries the Trivy server for a report on the given manifest.
// A caller must take care of closing ReportPayload.Contents like a Response.Body.
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

	resp, err := http.DefaultClient.Do(req) //nolint:bodyclose // caller is responsible for closing resp.Body
	if err != nil {
		return ReportPayload{}, err
	}
	if resp.StatusCode != http.StatusOK {
		// from inner to outer: cast to string, remove extra new lines, remove color escape codes, replace multiple consecutive spaces with one
		respBody, err := io.ReadAll(resp.Body)
		var respCleaned string
		if err == nil {
			respCleaned = strings.Join(strings.Fields(stripColor(strings.TrimSpace(string(respBody)))), " ")
		} else {
			respCleaned = "could not read body"
		}
		return ReportPayload{}, fmt.Errorf("trivy proxy did not return 200: %d %s", resp.StatusCode, respCleaned)
	}

	return ReportPayload{Format: format, Contents: resp.Body}, nil
}

// A regexp that matches ANSI escape sequences of the type SGR.
var ansiColorCodeRx = regexp.MustCompile("\x1B" + `\[[0-9;]*m`)

func stripColor(in string) string {
	return ansiColorCodeRx.ReplaceAllString(in, "")
}
