// SPDX-FileCopyrightText: 2024 SAP SE
// SPDX-License-Identifier: Apache-2.0

package models

import (
	"time"

	"github.com/opencontainers/go-digest"
)

type TrivySecurityInfo struct {
	RepositoryID        int64               `db:"repo_id"`
	Digest              digest.Digest       `db:"digest"`
	VulnerabilityStatus VulnerabilityStatus `db:"vuln_status"`
	Message             string              `db:"message"`
	NextCheckAt         time.Time           `db:"next_check_at"` // see tasks.CheckTrivySecurityStatusJob
	CheckedAt           *time.Time          `db:"checked_at"`
	CheckDurationSecs   *float64            `db:"check_duration_secs"`
}
