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
)

func BenchmarkEnrichReport(b *testing.B) {
	for b.Loop() {
		policies := SecurityScanPolicySet{}
		content := must.Return(os.ReadFile("../tasks/fixtures/trivy/report-vulnerable-with-fixes.json"))
		report := trivy.ReportPayload{
			Format:   "json",
			Contents: io.NopCloser(bytes.NewReader(content)),
		}
		_ = must.Return(policies.EnrichReport(&report, time.Now()))
	}
}
