// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"

	"github.com/sapcc/keppel/internal/trivy"

	"github.com/sapcc/go-bits/must"
	"github.com/sapcc/go-bits/regexpext"
)

func BenchmarkEnrichReportWithOneIgnore(b *testing.B) {
	b.ReportAllocs()

	reportContents := must.Return(os.ReadFile("../tasks/fixtures/trivy/report-vulnerable-with-fixes.json"))

	b.ResetTimer()
	for b.Loop() {
		policies := SecurityScanPolicySet{{
			VulnerabilityIDRx: regexpext.BoundedRegexp(`CVE-2022-29458`),
			Action: SecurityScanPolicyAction{
				Assessment: "This is a test policy.",
				Ignore:     true,
			},
		}}
		report := trivy.ReportPayload{
			Format:   "json",
			Contents: io.NopCloser(bytes.NewReader(reportContents)),
		}
		_ = must.Return(policies.EnrichReport(&report, time.Now()))
		must.Succeed(report.Contents.Close())
	}
}

func BenchmarkEnrichReportExceptFixReleased(b *testing.B) {
	b.ReportAllocs()

	reportContents := must.Return(os.ReadFile("../tasks/fixtures/trivy/report-vulnerable-with-fixes.json"))

	b.ResetTimer()
	for b.Loop() {
		policies := SecurityScanPolicySet{{
			VulnerabilityIDRx: regexpext.BoundedRegexp(`CVE-2022-29458`),
			Action: SecurityScanPolicyAction{
				Assessment: "This is a test policy.",
				Severity:   "Low",
			},
			ExceptFixReleased: true,
		}}
		report := trivy.ReportPayload{
			Format:   "json",
			Contents: io.NopCloser(bytes.NewReader(reportContents)),
		}
		_ = must.Return(policies.EnrichReport(&report, time.Now()))
		must.Succeed(report.Contents.Close())
	}
}
