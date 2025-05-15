// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sapcc/keppel/internal/models"
)

// GCPolicy is a policy enabling optional garbage collection runs in an account.
// It is stored in serialized form in the GCPoliciesJSON field of type Account.
type GCPolicy struct {
	PolicyMatch
	OnlyUntagged   bool              `json:"only_untagged,omitempty"`
	TimeConstraint *GCTimeConstraint `json:"time_constraint,omitempty"`
	Action         string            `json:"action"`
}

// GCTimeConstraint appears in type GCPolicy.
type GCTimeConstraint struct {
	FieldName   string   `json:"on"`
	OldestCount uint64   `json:"oldest,omitempty"`
	NewestCount uint64   `json:"newest,omitempty"`
	MinAge      Duration `json:"older_than,omitempty"`
	MaxAge      Duration `json:"newer_than,omitempty"`
}

// MatchesTags evaluates the tag regexes in this policy for a complete set of
// tag names belonging to a single manifest.
func (g GCPolicy) MatchesTags(tagNames []string) bool {
	if g.OnlyUntagged && len(tagNames) > 0 {
		return false
	}

	return g.PolicyMatch.MatchesTags(tagNames)
}

// MatchesTimeConstraint evaluates the time constraint in this policy for the
// given manifest. A full list of all manifests in this repo must be supplied in
// order to evaluate "newest" and "oldest" time constraints. The final argument
// must be equivalent to time.Now(); it is given explicitly to allow for
// simulated clocks during unit tests.
func (g GCPolicy) MatchesTimeConstraint(manifest models.Manifest, allManifestsInRepo []models.Manifest, now time.Time) bool {
	// do we have a time constraint at all?
	if g.TimeConstraint == nil {
		return true
	}
	tc := *g.TimeConstraint
	if tc.FieldName == "" {
		return true
	}

	// select the right time field
	var getTime func(models.Manifest) time.Time
	switch tc.FieldName {
	case "pushed_at":
		getTime = func(m models.Manifest) time.Time { return m.PushedAt }
	case "last_pulled_at":
		getTime = func(m models.Manifest) time.Time {
			if m.LastPulledAt == nil {
				return time.Unix(0, 0)
			}
			return *m.LastPulledAt
		}
	default:
		panic(fmt.Sprintf("unexpected GC policy time constraint target: %q (why was this not caught by Validate!?)", tc.FieldName))
	}
	getAge := func(m models.Manifest) Duration {
		return Duration(now.Sub(getTime(m)))
	}

	// option 1: simple threshold-based time constraint
	if tc.MinAge != 0 {
		return getAge(manifest) >= tc.MinAge
	}
	if tc.MaxAge != 0 {
		return getAge(manifest) <= tc.MaxAge
	}

	// option 2: order-based time constraint (we can skip all the sorting logic if we have less manifests than we want to match)
	if tc.OldestCount != 0 && uint64(len(allManifestsInRepo)) < tc.OldestCount {
		return true
	}
	if tc.NewestCount != 0 && uint64(len(allManifestsInRepo)) < tc.NewestCount {
		return true
	}

	// sort manifests by the right time field
	sort.Slice(allManifestsInRepo, func(i, j int) bool {
		lhs := allManifestsInRepo[i]
		rhs := allManifestsInRepo[j]
		return getAge(lhs) > getAge(rhs)
	})

	// which manifests match? (note that we already know that
	// len(allManifestsInRepo) is larger than the amount we want to match, so we
	// don't have to check bounds any further)
	var matchingManifests []models.Manifest
	switch {
	case tc.OldestCount != 0:
		matchingManifests = allManifestsInRepo[:tc.OldestCount]
	case tc.NewestCount != 0:
		matchingManifests = allManifestsInRepo[uint64(len(allManifestsInRepo))-tc.NewestCount:]
	default:
		panic("unexpected GC policy time constraint: no threshold configured (why was this not caught by Validate!?)")
	}
	for _, m := range matchingManifests {
		if m.Digest == manifest.Digest {
			return true
		}
	}
	return false
}

// Validate returns an error if this policy is invalid.
func (g GCPolicy) Validate() error {
	err := g.PolicyMatch.Validate()
	if err != nil {
		return err
	}

	if g.OnlyUntagged {
		if g.TagRx != "" {
			return errors.New(`GC policy cannot have the "match_tag" attribute when "only_untagged" is set`)
		}
		if g.NegativeTagRx != "" {
			return errors.New(`GC policy cannot have the "except_tag" attribute when "only_untagged" is set`)
		}
	}

	if g.TimeConstraint != nil {
		tc := *g.TimeConstraint
		var tcFilledFields []string
		if tc.OldestCount != 0 {
			tcFilledFields = append(tcFilledFields, `"oldest"`)
			if g.Action == "delete" {
				return fmt.Errorf(`GC policy with action %q cannot set the "time_constraint.oldest" attribute`, g.Action)
			}
		}
		if tc.NewestCount != 0 {
			tcFilledFields = append(tcFilledFields, `"newest"`)
			if g.Action == "delete" {
				return fmt.Errorf(`GC policy with action %q cannot set the "time_constraint.newest" attribute`, g.Action)
			}
		}
		if tc.MinAge != 0 {
			tcFilledFields = append(tcFilledFields, `"older_than"`)
		}
		if tc.MaxAge != 0 {
			tcFilledFields = append(tcFilledFields, `"newer_than"`)
		}

		switch tc.FieldName {
		case "":
			return errors.New(`GC policy time constraint must have the "on" attribute`)
		case "last_pulled_at", "pushed_at":
			if len(tcFilledFields) == 0 {
				return errors.New(`GC policy time constraint needs to set at least one attribute other than "on"`)
			}
			if len(tcFilledFields) > 1 {
				return fmt.Errorf(`GC policy time constraint cannot set all these attributes at once: %s`, strings.Join(tcFilledFields, ", "))
			}
		default:
			return fmt.Errorf(`%q is not a valid target for a GC policy time constraint`, tc.FieldName)
		}
	}

	switch g.Action {
	case "delete", "protect":
		// valid
		return nil
	case "":
		return errors.New(`GC policy must have the "action" attribute`)
	default:
		return fmt.Errorf("%q is not a valid action for a GC policy", g.Action)
	}
}

// ParseGCPolicies parses the GC policies for the given account.
func ParseGCPolicies(account models.Account) ([]GCPolicy, error) {
	if account.GCPoliciesJSON == "" || account.GCPoliciesJSON == "[]" {
		return nil, nil
	}
	var policies []GCPolicy
	err := json.Unmarshal([]byte(account.GCPoliciesJSON), &policies)
	return policies, err
}

// GCStatus documents the current status of a manifest with regard to image GC.
// It is stored in serialized form in the GCStatusJSON field of type Manifest.
//
// Since GCStatus objects describe images that currently exist in the DB, they
// only describe policy decisions that led to no cleanup.
type GCStatus struct {
	// True if the manifest was uploaded less than 10 minutes ago and is therefore
	// protected from GC.
	ProtectedByRecentUpload bool `json:"protected_by_recent_upload,omitempty"`
	// If a parent manifest references this manifest and thus protects it from GC,
	// this contains the parent manifest's digest.
	ProtectedByParentManifest string `json:"protected_by_parent,omitempty"`
	// If this manifest references a subject and is thus protected from GC,
	// this contains the subject's digest.
	ProtectedBySubjectManifest string `json:"protected_by_subject,omitempty"`
	// If a policy with action "protect" applies to this image,
	// this contains the definition of the policy.
	ProtectedByPolicy *GCPolicy `json:"protected_by_policy,omitempty"`
	// If the image is not protected, contains all policies with action "delete"
	// that could delete this image in the future.
	RelevantPolicies []GCPolicy `json:"relevant_policies,omitempty"`
}

// IsProtected returns whether any of the ProtectedBy... fields is filled.
func (s GCStatus) IsProtected() bool {
	return s.ProtectedByRecentUpload || s.ProtectedByParentManifest != "" || s.ProtectedBySubjectManifest != "" || s.ProtectedByPolicy != nil
}
