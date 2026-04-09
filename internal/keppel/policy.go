// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"errors"

	"slices"

	"github.com/sapcc/go-bits/regexpext"

	"github.com/sapcc/keppel/internal/models"
)

// PolicyMatchRule contains the matching rules that are shared by type GCPolicy and TagPolicy.
type PolicyMatchRule struct {
	RepositoryRx         regexpext.BoundedRegexp `json:"match_repository"`
	NegativeRepositoryRx regexpext.BoundedRegexp `json:"except_repository,omitempty"`
	TagRx                regexpext.BoundedRegexp `json:"match_tag,omitempty"`
	NegativeTagRx        regexpext.BoundedRegexp `json:"except_tag,omitempty"`
}

// MatchesRepository evaluates the repository regexes in this policy.
func (p PolicyMatchRule) MatchesRepository(repoName models.RepositoryName) bool {
	//NOTE: NegativeRepositoryRx takes precedence and is thus evaluated first.
	if p.NegativeRepositoryRx != "" && p.NegativeRepositoryRx.MatchString(string(repoName)) {
		return false
	}
	return p.RepositoryRx.MatchString(string(repoName))
}

// MatchesTags evaluates the tag regexes in this policy for a complete set of
// tag names belonging to a single manifest.
func (p PolicyMatchRule) MatchesTags(tagNames []string) bool {
	//NOTE: NegativeTagRx takes precedence over TagRx and is thus evaluated first.
	if p.NegativeTagRx != "" {
		if slices.ContainsFunc(tagNames, p.NegativeTagRx.MatchString) {
			return false
		}
	}
	if p.TagRx != "" {
		if slices.ContainsFunc(tagNames, p.TagRx.MatchString) {
			return true
		}
	}

	// if we did not have any matching tags, the match is successful unless we
	// required a positive tag match
	return p.TagRx == ""
}

// Validate returns an error if this policy is invalid.
func (p PolicyMatchRule) validate(context string) error {
	if p.RepositoryRx == "" {
		return errors.New(context + ` must have the "match_repository" attribute`)
	}

	return nil
}
