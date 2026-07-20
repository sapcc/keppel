// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package models

import (
	"time"

	"github.com/opencontainers/go-digest"
	. "go.xyrillian.de/gg/option"
	"go.xyrillian.de/oblast"
)

// TrivySecurityInfo contains a record from the `trivy_security_info` table.
type TrivySecurityInfo struct {
	RepositoryID                 int64               `db:"repo_id"`
	Digest                       digest.Digest       `db:"digest"`
	VulnerabilityStatus          VulnerabilityStatus `db:"vuln_status"`
	VulnerabilityStatusChangedAt Option[time.Time]   `db:"vuln_status_changed_at"`
	Message                      string              `db:"message"`
	NextCheckAt                  Option[time.Time]   `db:"next_check_at"` // see tasks.CheckTrivySecurityStatusJob
	CheckedAt                    Option[time.Time]   `db:"checked_at"`
	CheckDurationSecs            Option[float64]     `db:"check_duration_secs"`

	// Whether a report with `--format json` is stored for this manifest.
	HasEnrichedReport bool `db:"has_enriched_report"`
}

// TrivySecurityInfoStore provides loading and storing of [TrivySecurityInfo] objects from the DB.
var TrivySecurityInfoStore = oblast.MustNewStore[TrivySecurityInfo](
	oblast.PostgresDialect(),
	oblast.TableNameIs("trivy_security_info"),
	oblast.PrimaryKeyIs("repo_id", "digest"),
)

// TrivySecurityInfoByDigest is an [oblast.RuntimeIndex] sorting [TrivySecurityInfo] by digest.
var TrivySecurityInfoByDigest = oblast.NewRuntimeIndex(func(t TrivySecurityInfo) digest.Digest { return t.Digest })
