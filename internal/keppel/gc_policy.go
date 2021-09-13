/*******************************************************************************
*
* Copyright 2021 SAP SE
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

package keppel

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

//GCPolicy is a policy enabling optional garbage collection runs in an account.
type GCPolicy struct {
	RepositoryPattern         string            `json:"match_repository"`
	NegativeRepositoryPattern string            `json:"except_repository,omitempty"`
	TagPattern                string            `json:"match_tag,omitempty"`
	NegativeTagPattern        string            `json:"except_tag,omitempty"`
	OnlyUntagged              bool              `json:"only_untagged,omitempty"`
	TimeConstraint            *GCTimeConstraint `json:"time_constraint,omitempty"`
	Action                    string            `json:"action"`
}

//GCTimeConstraint appears in type GCPolicy.
type GCTimeConstraint struct {
	FieldName   string   `json:"on"`
	OldestCount uint64   `json:"oldest,omitempty"`
	NewestCount uint64   `json:"newest,omitempty"`
	MinAge      Duration `json:"older_than,omitempty"`
	MaxAge      Duration `json:"newer_than,omitempty"`
}

//MatchesRepository evaluates the repository regexes in this policy.
func (g GCPolicy) MatchesRepository(repoName string) bool {
	//Notes:
	//- Regex parse errors always make the match fail to avoid accidental overmatches.
	//- NegativeRepositoryPattern takes precedence and is thus evaluated first.

	rx, err := regexp.Compile(fmt.Sprintf(`^%s$`, g.NegativeRepositoryPattern))
	if err != nil || rx.MatchString(repoName) {
		return false
	}

	rx, err = regexp.Compile(fmt.Sprintf(`^%s$`, g.RepositoryPattern))
	return err == nil && rx.MatchString(repoName)
}

//Validate returns an error if this policy is invalid.
func (g GCPolicy) Validate() error {
	if g.RepositoryPattern == "" {
		return errors.New(`GC policy must have the "match_repository" attribute`)
	}

	for _, pattern := range []string{g.RepositoryPattern, g.NegativeRepositoryPattern, g.TagPattern, g.NegativeTagPattern} {
		if pattern == "" {
			continue
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("%q is not a valid regex: %s", pattern, err.Error())
		}
	}

	if g.OnlyUntagged {
		if g.TagPattern != "" {
			return fmt.Errorf(`GC policy cannot have the "match_tag" attribute when "only_untagged" is set`)
		}
		if g.NegativeTagPattern != "" {
			return fmt.Errorf(`GC policy cannot have the "except_tag" attribute when "only_untagged" is set`)
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
				return fmt.Errorf(`GC policy time constraint needs to set at least one attribute other than "on"`)
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
		//valid
		return nil
	case "":
		return errors.New(`GC policy must have the "action" attribute`)
	default:
		return fmt.Errorf("%q is not a valid action for a GC policy", g.Action)
	}
}

//ParseGCPolicies parses the GC policies for the given account.
func (a Account) ParseGCPolicies() ([]GCPolicy, error) {
	if a.GCPoliciesJSON == "" || a.GCPoliciesJSON == "[]" {
		return nil, nil
	}
	var policies []GCPolicy
	err := json.Unmarshal([]byte(a.GCPoliciesJSON), &policies)
	return policies, err
}
