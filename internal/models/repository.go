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
)

// Repository contains a record from the `repos` table.
type Repository struct {
	ID                      int64       `db:"id"`
	AccountName             AccountName `db:"account_name"`
	Name                    string      `db:"name"`
	NextBlobMountSweepAt    *time.Time  `db:"next_blob_mount_sweep_at"` // see tasks.BlobMountSweepJob
	NextManifestSyncAt      *time.Time  `db:"next_manifest_sync_at"`    // see tasks.ManifestSyncJob (only set for replica accounts)
	NextGarbageCollectionAt *time.Time  `db:"next_gc_at"`               // see tasks.GarbageCollectManifestsJob
}

// FullName prepends the account name to the repository name.
func (r Repository) FullName() string {
	return string(r.AccountName) + `/` + r.Name
}
