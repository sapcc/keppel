// Copyright 2023 SAP SE
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package processor

import (
	"context"

	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/clair"
)

func (p *Processor) SetManifestAndParentsToPending(ctx context.Context, manifestDigest string) error {
	err := p.cfg.ClairClient.DeleteManifest(ctx, manifestDigest)
	if err != nil {
		return err
	}

	_, err = p.db.Exec(sqlext.SimplifyWhitespace(`
		UPDATE vuln_info SET status = $1, index_state = '', next_check_at = $2
		WHERE digest = $3 OR digest IN (
			SELECT parent_digest FROM manifest_manifest_refs WHERE child_digest = $3
	)`), clair.PendingVulnerabilityStatus, p.timeNow(), manifestDigest)

	return err
}
