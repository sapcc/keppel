// SPDX-FileCopyrightText: 2021 SAP SE
// SPDX-License-Identifier: Apache-2.0

package auth

import "github.com/sapcc/keppel/internal/keppel"

// Authorization describes the access rights of a particular user session, i.e.
// in the scope of an individual API request.
type Authorization struct {
	// UserIdentity identifies the user that sent the request.
	UserIdentity keppel.UserIdentity
	// ScopeSet identifies the permissions granted to the user for the duration of
	// this request.
	ScopeSet ScopeSet
	// Audience identifies the API endpoint where the user sent the request.
	Audience Audience
}
