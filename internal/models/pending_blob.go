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

// PendingBlob contains a record from the `pending_blobs` table.
type PendingBlob struct {
	AccountName  string        `db:"account_name"`
	Digest       digest.Digest `db:"digest"`
	Reason       PendingReason `db:"reason"`
	PendingSince time.Time     `db:"since"`
}

// PendingReason is an enum that explains why a blob is pending.
type PendingReason string

const (
	// PendingBecauseOfReplication is when a blob is pending because
	// it is currently being replicated from an upstream registry.
	PendingBecauseOfReplication PendingReason = "replication"
)
