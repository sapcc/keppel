/******************************************************************************
*
*  Copyright 2023 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package keppel

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/aquasecurity/trivy/pkg/types"
	"github.com/sapcc/go-bits/errext"
	"github.com/sapcc/go-bits/regexpext"

	"github.com/sapcc/keppel/internal/clair"
	"github.com/sapcc/keppel/internal/trivy"
)

// SecurityScanPolicy is a policy enabling user-defined adjustments to
// vulnerability reports generated by Trivy.
type SecurityScanPolicy struct {
	//NOTE: We have code that uses slices.Contains() to locate policies. Be careful
	//when adding fields that cannot be meaningfully compared with the == operator.
	ManagingUserName          string                   `json:"managed_by_user,omitempty"`
	RepositoryRx              regexpext.BoundedRegexp  `json:"match_repository"`
	NegativeRepositoryRx      regexpext.BoundedRegexp  `json:"except_repository,omitempty"`
	VulnerabilityIDRx         regexpext.BoundedRegexp  `json:"match_vulnerability_id"`
	NegativeVulnerabilityIDRx regexpext.BoundedRegexp  `json:"except_vulnerability_id,omitempty"`
	ExceptFixReleased         bool                     `json:"except_fix_released,omitempty"`
	Action                    SecurityScanPolicyAction `json:"action"`
}

// SecurityScanPolicyAction appears in type SecurityScanPolicy.
type SecurityScanPolicyAction struct {
	Assessment string                    `json:"assessment"`
	Ignore     bool                      `json:"ignore,omitempty"`
	Severity   clair.VulnerabilityStatus `json:"severity,omitempty"`
}

// String returns the JSON representation of this policy (for use in log and
// error messages).
func (p SecurityScanPolicy) String() string {
	//we only obtain SecurityScanPolicy instances through unmarshaling, so it is
	//safe to assume that they will marshal without error
	buf, err := json.Marshal(p)
	if err != nil {
		panic(err.Error())
	}
	return string(buf)
}

// Validate returns errors if this policy is invalid.
//
// When constructing error messages, `path` is prepended to all field names.
// This allows identifying the location of the policy within a larger data structure.
func (p SecurityScanPolicy) Validate(path string) (errs errext.ErrorSet) {
	if path == "" {
		path = "policy"
	}

	if p.RepositoryRx == "" {
		errs.Addf(`%s must have the "match_repository" attribute`, path)
	}
	if p.VulnerabilityIDRx == "" {
		errs.Addf(`%s must have the "match_vulnerability_id" attribute`, path)
	}

	if p.Action.Assessment == "" {
		errs.Addf(`%s.action must have the "assessment" attribute`, path)
	}
	if len(p.Action.Assessment) > 1024 {
		errs.Addf(`%s.action.assessment cannot be larger than 1 KiB`, path)
	}

	if p.Action.Ignore {
		if p.Action.Severity != "" {
			errs.Addf(`%s.action cannot have the "severity" attribute when "ignore" is set`, path)
		}
	} else {
		if p.Action.Severity == "" {
			errs.Addf(`%s.action must have the "severity" attribute when "ignore" is not set`, path)
		} else if !isSeverityKnownByTrivy(p.Action.Severity) {
			errs.Addf(`%s.action.severity contains the invalid value %q`, path, p.Action.Severity)
		}
	}

	return
}

func isSeverityKnownByTrivy(severity clair.VulnerabilityStatus) bool {
	// We don't allow downgrading a severity to "Unknown" through a policy.
	if severity == clair.UnknownSeverity {
		return false
	}
	for _, vulnStatus := range clair.MapToTrivySeverity {
		if severity == vulnStatus {
			return true
		}
	}
	return false
}

// VulnerabilityStatus returns the status that this policy forces for matching
// vulnerabilities in matching repos.
func (p SecurityScanPolicy) VulnerabilityStatus() clair.VulnerabilityStatus {
	//NOTE: Validate() ensures that either `Action.Ignore` or `Action.Severity` is set.
	if p.Action.Ignore {
		return clair.CleanSeverity
	}
	return p.Action.Severity
}

// MatchesRepository evaluates the repository regexes in this policy.
func (p SecurityScanPolicy) MatchesRepository(repo Repository) bool {
	//NOTE: NegativeRepositoryRx takes precedence and is thus evaluated first.
	if p.NegativeRepositoryRx != "" && p.NegativeRepositoryRx.MatchString(repo.Name) {
		return false
	}
	return p.RepositoryRx.MatchString(repo.Name)
}

// MatchesVulnerability evaluates the vulnerability regexes and checkin this policy.
func (p SecurityScanPolicy) MatchesVulnerability(vuln types.DetectedVulnerability) bool {
	if p.ExceptFixReleased && trivy.FixIsReleased(vuln) {
		return false
	}

	//NOTE: NegativeRepositoryRx takes precedence and is thus evaluated first.
	if p.NegativeVulnerabilityIDRx != "" && p.NegativeVulnerabilityIDRx.MatchString(vuln.VulnerabilityID) {
		return false
	}
	return p.VulnerabilityIDRx.MatchString(vuln.VulnerabilityID)
}

// SecurityScanPolicySet contains convenience functions for operating on a list
// of SecurityScanPolicy (like those found in Account.SecurityScanPoliciesJSON).
type SecurityScanPolicySet []SecurityScanPolicy

// SecurityScanPoliciesFor deserializes this account's security scan policies
// and returns the subset that match the given repository.
func (a Account) SecurityScanPoliciesFor(repo Repository) (SecurityScanPolicySet, error) {
	if repo.AccountName != a.Name {
		//defense in depth
		panic(fmt.Sprintf(
			"Account.SecurityScanPoliciesFor called with repo.AccountName = %q, but a.Name = %q!",
			repo.AccountName, a.Name))
	}

	var policies SecurityScanPolicySet
	err := json.Unmarshal([]byte(a.SecurityScanPoliciesJSON), &policies)
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshal SecurityScanPoliciesJSON for account %q: %w", a.Name, err)
	}

	var result SecurityScanPolicySet
	for _, p := range policies {
		if p.MatchesRepository(repo) {
			result = append(result, p)
		}
	}
	return result, nil
}

// PolicyForVulnerability returns the first policy from this set that matches
// the vulnerability, or nil if no policy matches.
func (s SecurityScanPolicySet) PolicyForVulnerability(vuln types.DetectedVulnerability) *SecurityScanPolicy {
	for _, p := range s {
		if p.MatchesVulnerability(vuln) {
			return &p
		}
	}
	return nil
}

// EnrichReport computes and inserts the "X-Keppel-Applicable-Policies" field
// if the report is `--format json`. Other formats are not altered.
func (s SecurityScanPolicySet) EnrichReport(payload *trivy.ReportPayload) error {
	if payload.Format != "json" {
		return nil
	}

	//decode relevant fields from report
	var parsedReport types.Report
	err := json.Unmarshal(payload.Contents, &parsedReport)
	if err != nil {
		return fmt.Errorf("cannot parse Trivy vulnerability report: %w", err)
	}

	//compute X-Keppel-Applicable-Policies set
	applicablePolicies := make(map[string]SecurityScanPolicy)
	for _, result := range parsedReport.Results {
		for _, vuln := range result.Vulnerabilities {
			policy := s.PolicyForVulnerability(vuln)
			if policy != nil {
				applicablePolicies[vuln.VulnerabilityID] = *policy
			}
		}
	}
	if len(applicablePolicies) == 0 {
		//the report is fine as-is
		return nil
	}

	//we cannot just update and remarshal `parsedReport` because it covers only a
	//small fraction of the full report; the following adds our custom field to
	//the report nondestructively
	//
	//NOTE: This is not using json.Unmarshal() + update + json.Marshal() because
	//that reorders fields in the existing report, which then breaks
	//assert.JSONFixtureFile matching. Because I don't want to rebuild how
	//assert.JSONFixtureFile works, this appends a field to the report without
	//unmarshalling it:
	//
	//    {...} -> {...,"X-Keppel-Applicable-Policies":{...}}
	applicablePoliciesJSON, err := json.Marshal(applicablePolicies)
	if err != nil {
		return fmt.Errorf("cannot marshal X-Keppel-Applicable-Policies: %w", err)
	}
	payload.Contents = bytes.Join([][]byte{
		//this trim cannot fail because we already did a successful json.Unmarshal above
		bytes.TrimSuffix(bytes.TrimSpace(payload.Contents), []byte("}")),
		[]byte(`,"X-Keppel-Applicable-Policies":`),
		applicablePoliciesJSON,
		[]byte("}\n"),
	}, nil)

	return nil
}
