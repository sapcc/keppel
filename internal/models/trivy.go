// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package models

import (
	"time"

	. "github.com/majewsky/gg/option"
	"github.com/opencontainers/go-digest"
)

// TrivySecurityInfo contains a record from the `trivy_security_info` table.
type TrivySecurityInfo struct {
	RepositoryID        int64               `db:"repo_id"`
	Digest              digest.Digest       `db:"digest"`
	VulnerabilityStatus VulnerabilityStatus `db:"vuln_status"`
	Message             string              `db:"message"`
	NextCheckAt         time.Time           `db:"next_check_at"` // see tasks.CheckTrivySecurityStatusJob
	CheckedAt           Option[time.Time]   `db:"checked_at"`
	CheckDurationSecs   Option[float64]     `db:"check_duration_secs"`

	// Whether a report with `--format json` is stored for this manifest.
	HasEnrichedReport bool `db:"has_enriched_report"`
}
