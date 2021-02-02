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

package clair

//VulnerabilityStatus enumerates the possible values for a manifest's vulnerability status.
type VulnerabilityStatus string

const (
	//ErrorVulnerabilityStatus is a VulnerabilityStatus that indicates that vulnerability scanning failed.
	ErrorVulnerabilityStatus VulnerabilityStatus = "Error"
	//PendingVulnerabilityStatus is a VulnerabilityStatus which means that we're not done scanning vulnerabilities yet.
	PendingVulnerabilityStatus VulnerabilityStatus = "Pending"
	//CleanSeverity is a VulnerabilityStatus which means that there are no vulnerabilities.
	CleanSeverity VulnerabilityStatus = "Clean"
	//UnknownSeverity is a VulnerabilityStatus which means that there are vulnerabilities, but their severity is unknown.
	UnknownSeverity VulnerabilityStatus = "Unknown"
	//NegligibleSeverity is a VulnerabilityStatus.
	NegligibleSeverity VulnerabilityStatus = "Negligible"
	//LowSeverity is a VulnerabilityStatus.
	LowSeverity VulnerabilityStatus = "Low"
	//MediumSeverity is a VulnerabilityStatus.
	MediumSeverity VulnerabilityStatus = "Medium"
	//HighSeverity is a VulnerabilityStatus.
	HighSeverity VulnerabilityStatus = "High"
	//CriticalSeverity is a VulnerabilityStatus.
	CriticalSeverity VulnerabilityStatus = "Critical"
	//Defcon1Severity is a VulnerabilityStatus.
	Defcon1Severity VulnerabilityStatus = "Defcon1"
)

var sevMap = map[VulnerabilityStatus]uint{
	ErrorVulnerabilityStatus:   0,
	PendingVulnerabilityStatus: 0,
	CleanSeverity:              1,
	UnknownSeverity:            2,
	NegligibleSeverity:         3,
	LowSeverity:                4,
	MediumSeverity:             5,
	HighSeverity:               6,
	CriticalSeverity:           7,
	Defcon1Severity:            8,
}

//MergeVulnerabilityStatuses combines multiple VulnerabilityStatus values into one.
//
//* Any ErrorVulnerabilityStatus input results in an ErrorVulnerabilityStatus result.
//* Otherwise, any PendingVulnerabilityStatus input results in a PendingVulnerabilityStatus result.
//* Otherwise, the result is the same as the highest individual severity.
func MergeVulnerabilityStatuses(sevs ...VulnerabilityStatus) VulnerabilityStatus {
	result := CleanSeverity
	for _, s := range sevs {
		//if any part of vulnerability scanning fails, the entirety is considered failed
		if s == ErrorVulnerabilityStatus {
			return ErrorVulnerabilityStatus
		}
		if sevMap[s] == 0 {
			//`s == PendingVulnerabilityStatus` or `s` is a string value not known to us
			return PendingVulnerabilityStatus
		}
		if sevMap[s] > sevMap[result] {
			result = s
		}
	}
	return result
}
