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
	"fmt"
	"regexp"
)

//GCPolicy is a policy enabling optional garbage collection runs in an account.
type GCPolicy struct {
	RepositoryPattern         string `json:"match_repository"`
	NegativeRepositoryPattern string `json:"except_repository,omitempty"`
	Strategy                  string `json:"strategy"`
}

//Matches evaluates the regexes in this policy.
func (g GCPolicy) Matches(repoName string) bool {
	//Notes:
	//- Regex parse errors always make the match fail to avoid accidental overmatches.
	//- NegativeRepositoryPattern takes precedence and is thus evaluated first
	rx, err := regexp.Compile(fmt.Sprintf(`^%s$`, g.NegativeRepositoryPattern))
	if err != nil || rx.MatchString(repoName) {
		return false
	}

	rx, err = regexp.Compile(fmt.Sprintf(`^%s$`, g.RepositoryPattern))
	return err == nil && rx.MatchString(repoName)
}
