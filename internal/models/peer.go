// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package models

import "time"

// Peer contains a record from the `peers` table.
type Peer struct {
	HostName string `db:"hostname"`

	UseForPullDelegation bool `db:"use_for_pull_delegation"`

	// OurPassword is what we use to log in at the peer.
	OurPassword string `db:"our_password"`

	// TheirCurrentPasswordHash and TheirPreviousPasswordHash is what the peer
	// uses to log in with us. Passwords are rotated every 10min. We allow access with
	// the current *and* the previous password to avoid a race where we enter the
	// new password in the database and then reject authentication attempts from
	// the peer before we told them about the new password.
	TheirCurrentPasswordHash  string `db:"their_current_password_hash"`
	TheirPreviousPasswordHash string `db:"their_previous_password_hash"`

	// LastPeeredAt is when we last issued a new password for this peer.
	LastPeeredAt *time.Time `db:"last_peered_at"` // see tasks.IssueNewPasswordForPeer
}
