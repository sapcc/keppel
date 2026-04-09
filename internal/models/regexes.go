// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package models

import (
	"regexp"
	"strings"

	. "github.com/majewsky/gg/option"
)

var (
	// repoPathComponentPattern is a regex string that validates individual path elements within repo names.
	repoPathComponentPattern = `[a-z0-9]+(?:[._-][a-z0-9]+)*`

	// repoPathComponentRx validates individual path elements within repo names.
	// A full repo name is made up of any such number of path components, separated by slashes.
	repoPathComponentRx = regexp.MustCompile(`^` + repoPathComponentPattern + `$`)
)

// The "with leading slash" simplifies the regex because we don't need to write the
// regex for a path element twice.
// Examples:
// - /library/alpine
// - /library/alpine/nonsense
var (
	repoNameWithLeadingSlashPattern = "(?:/" + repoPathComponentPattern + ")+"
	repoNameWithLeadingSlashRx      = regexp.MustCompile(`^` + repoNameWithLeadingSlashPattern + `$`)
)

// ImageReferenceRx is used to match repo/account and optional tag and digest combination
// Examples:
// - /library/alpine
// - /library/alpine:nonsense
// - /library/alpine:e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef
// - /library/alpine@sha256:e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef
// - /library/alpine:nonsense@sha256:e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef
var imageReferenceRx = regexp.MustCompile(`^(` + repoNameWithLeadingSlashPattern + `)(?::([a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}))?(?:@(sha256:[a-z0-9]{64}))?$`)

// CheckAccountName casts the given string into [AccountName].
// If the input is a well-formed account name, [None] is returned.
// This does not check whether the account actually exists in the DB.
func CheckAccountName(input string) Option[AccountName] {
	// account names are historically limited to 48 chars (because we used to put
	// them in Postgres database names which are limited to 64 chars); we might
	// lift this restriction in the future, but there is no immediate need for it
	if len(input) > 48 {
		return None[AccountName]()
	}
	if !repoPathComponentRx.MatchString(input) {
		return None[AccountName]()
	}
	return Some(AccountName(input))
}

// CheckRepositoryName casts the given string into [RepositoryName].
// If the input is not a well-formed repository name, [None] is returned.
// This does not check whether the repository actually exists in the DB.
func CheckRepositoryName(input string) Option[RepositoryName] {
	if input == "" {
		return None[RepositoryName]()
	}
	for pathComponent := range strings.SplitSeq(input, `/`) {
		if !repoPathComponentRx.MatchString(pathComponent) {
			return None[RepositoryName]()
		}
	}
	return Some(RepositoryName(input))
}
