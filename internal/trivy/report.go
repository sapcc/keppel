/*******************************************************************************
*
* Copyright 2023 SAP SE
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

package trivy

// VulnerabilityReport contains selected fields from the Trivy vulnerability
// report (i.e. what Trivy calls `--format json`).
type VulnerabilityReport struct {
	Results []struct {
		Vulnerabilities []ReportedVulnerability `json:"Vulnerabilities"`
	} `json:"Results"`
}

// ReportedVulnerability contains selected fields for the
// `Results.Vulnerabilities[]` field of a Trivy vulnerability report.
type ReportedVulnerability struct {
	VulnerabilityID string `json:"VulnerabilityID"` // e.g. "CVE-2011-3374"
	Severity        string `json:"Severity"`        // e.g. "HIGH", cf. clair.MapToTrivySeverity
	FixedVersion    string `json:"FixedVersion"`    // e.g. "65.5.1"
}

// FixIsReleased returns whether FixedVersion is non-empty. (This particular
// method name reads better in some situations than `FixedVersion != ""`.)
func (v ReportedVulnerability) FixIsReleased() bool {
	return v.FixedVersion != ""
}
