/*******************************************************************************
*
* Copyright 2024 SAP SE
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
