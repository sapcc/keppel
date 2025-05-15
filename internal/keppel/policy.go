// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"errors"

	"github.com/sapcc/go-bits/regexpext"
)

type PolicyMatch struct {
	RepositoryRx         regexpext.BoundedRegexp `json:"match_repository"`
	NegativeRepositoryRx regexpext.BoundedRegexp `json:"except_repository,omitempty"`
	TagRx                regexpext.BoundedRegexp `json:"match_tag,omitempty"`
	NegativeTagRx        regexpext.BoundedRegexp `json:"except_tag,omitempty"`
}

// MatchesRepository evaluates the repository regexes in this policy.
func (p PolicyMatch) MatchesRepository(repoName string) bool {
	//NOTE: NegativeRepositoryRx takes precedence and is thus evaluated first.
	if p.NegativeRepositoryRx != "" && p.NegativeRepositoryRx.MatchString(repoName) {
		return false
	}
	return p.RepositoryRx.MatchString(repoName)
}

// MatchesTags evaluates the tag regexes in this policy for a complete set of
// tag names belonging to a single manifest.
func (p PolicyMatch) MatchesTags(tagNames []string) bool {
	//NOTE: NegativeTagRx takes precedence over TagRx and is thus evaluated first.
	if p.NegativeTagRx != "" {
		for _, tagName := range tagNames {
			if p.NegativeTagRx.MatchString(tagName) {
				return false
			}
		}
	}
	if p.TagRx != "" {
		for _, tagName := range tagNames {
			if p.TagRx.MatchString(tagName) {
				return true
			}
		}
	}

	// if we did not have any matching tags, the match is successful unless we
	// required a positive tag match
	return p.TagRx == ""
}

// Validate returns an error if this policy is invalid.
func (p PolicyMatch) Validate() error {
	if p.RepositoryRx == "" {
		return errors.New(`GC policy must have the "match_repository" attribute`)
	}

	return nil
}
