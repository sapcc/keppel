// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"os"
	"testing"
	"time"

	"github.com/sapcc/keppel/internal/trivy"

	"github.com/sapcc/go-bits/must"
)

func BenchmarkEnrichReport(b *testing.B) {
	for b.Loop() {
		policies := SecurityScanPolicySet{}
		report := trivy.ReportPayload{
			Format:   "json",
			Contents: must.Return(os.Open("../tasks/fixtures/trivy/report-vulnerable-with-fixes.json")),
		}
		_ = must.Return(policies.EnrichReport(&report, time.Now()))
		must.Succeed(report.Contents.Close())
	}
}
