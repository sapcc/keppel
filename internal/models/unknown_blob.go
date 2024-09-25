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

// UnknownBlob contains a record from the `unknown_blobs` table.
// This is only used by tasks.StorageSweepJob().
type UnknownBlob struct {
	AccountName    AccountName `db:"account_name"`
	StorageID      string      `db:"storage_id"`
	CanBeDeletedAt time.Time   `db:"can_be_deleted_at"`
}

// UnknownManifest contains a record from the `unknown_manifests` table.
// This is only used by tasks.StorageSweepJob().
//
// NOTE: We don't use repository IDs here because unknown manifests may exist in
// repositories that are also not known to the database.
type UnknownManifest struct {
	AccountName    AccountName   `db:"account_name"`
	RepositoryName string        `db:"repo_name"`
	Digest         digest.Digest `db:"digest"`
	CanBeDeletedAt time.Time     `db:"can_be_deleted_at"`
}
