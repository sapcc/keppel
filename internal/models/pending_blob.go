// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package models

import (
	"time"

	"github.com/opencontainers/go-digest"
	"go.xyrillian.de/oblast"
)

// PendingBlob contains a record from the `pending_blobs` table.
type PendingBlob struct {
	AccountName  AccountName   `db:"account_name"`
	Digest       digest.Digest `db:"digest"`
	Reason       PendingReason `db:"reason"`
	PendingSince time.Time     `db:"since"`
}

// PendingBlobStore provides loading and storing of [PendingBlob] objects from the DB.
var PendingBlobStore = oblast.MustNewStore[PendingBlob](
	oblast.PostgresDialect(),
	oblast.TableNameIs("pending_blobs"),
	oblast.PrimaryKeyIs("account_name", "digest"),
)

// PendingReason is an enum that explains why a blob is pending.
type PendingReason string

const (
	// PendingBecauseOfReplication is when a blob is pending because
	// it is currently being replicated from an upstream registry.
	PendingBecauseOfReplication PendingReason = "replication"
)
