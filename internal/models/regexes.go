// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package models

import (
	"regexp"
)

var (
	RepoNameRx          = `[a-z0-9]+(?:[._-][a-z0-9]+)*`
	RepoPathRx          = regexp.MustCompile(`^` + RepoNameRx + `(?:/` + RepoNameRx + `)*$`)
	RepoPathComponentRx = regexp.MustCompile(`^` + RepoNameRx + `$`)
)

// The "with leading slash" simplifies the regex because we don't need to write the
// regex for a path element twice.
// Examples:
// - /library/alpine
// - /library/alpine/nonsense
var (
	RepoNameWithLeadingSlash   = "(?:/" + RepoNameRx + ")+"
	RepoNameWithLeadingSlashRx = regexp.MustCompile(`^` + RepoNameWithLeadingSlash + `$`)
)

// ImageReferenceRx is used to match repo/account and optional tag and digest combination
// Examples:
// - /library/alpine
// - /library/alpine:nonsense
// - /library/alpine:e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef
// - /library/alpine@sha256:e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef
// - /library/alpine:nonsense@sha256:e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef
var ImageReferenceRx = regexp.MustCompile(`^(` + RepoNameWithLeadingSlash + `)(?::([a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}))?(?:@(sha256:[a-z0-9]{64}))?$`)

// IsAccountName returns whether the given string is a well-formed account name.
// This does not check whether the account actually exists in the DB.
func IsAccountName(input string) bool {
	// account names are historically limited to 48 chars (because we used to put
	// them in Postgres database names which are limited to 64 chars); we might
	// lift this restriction in the future, but there is no immediate need for it
	if len(input) > 48 {
		return false
	}
	return RepoPathComponentRx.MatchString(input)
}
