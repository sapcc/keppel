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

//Severity enumerates the severity values understood by Clair.
type Severity string

const (
	//UnknownSeverity is a Severity.
	UnknownSeverity Severity = "Unknown"
	//CleanSeverity is a Severity. This severity means that there are no vulnerabilities.
	CleanSeverity Severity = "Clean"
	//NegligibleSeverity is a Severity.
	NegligibleSeverity Severity = "Negligible"
	//LowSeverity is a Severity.
	LowSeverity Severity = "Low"
	//MediumSeverity is a Severity.
	MediumSeverity Severity = "Medium"
	//HighSeverity is a Severity.
	HighSeverity Severity = "High"
	//CriticalSeverity is a Severity.
	CriticalSeverity Severity = "Critical"
	//Defcon1Severity is a Severity.
	Defcon1Severity Severity = "Defcon1"
)

var sevMap = map[Severity]uint{
	UnknownSeverity:    0,
	CleanSeverity:      1,
	NegligibleSeverity: 2,
	LowSeverity:        3,
	MediumSeverity:     4,
	HighSeverity:       5,
	CriticalSeverity:   6,
	Defcon1Severity:    7,
}

//MergeSeverities combines multiple severities into one value. The result is
//the same as the highest individual severity. As an exception, UnknownSeverity
//is returned if any individual severity is UnknownSeverity.
func MergeSeverities(sevs ...Severity) Severity {
	result := CleanSeverity
	for _, s := range sevs {
		if sevMap[s] == 0 {
			//`s == UnknownSeverity` or `s` is a string value not known to us
			return UnknownSeverity
		}
		if sevMap[s] > sevMap[result] {
			result = s
		}
	}
	return result
}
