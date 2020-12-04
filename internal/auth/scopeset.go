/*******************************************************************************
*
* Copyright 2020 SAP SE
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

package auth

//ScopeSet is a set of scopes.
type ScopeSet []*Scope

//Add adds a scope to this ScopeSet. If the ScopeSet already contains a Scope
//referring to the same resource, it is merged with the given scope.
func (ss *ScopeSet) Add(s Scope) {
	for _, other := range *ss {
		if s.ResourceType == other.ResourceType && s.ResourceName == other.ResourceName {
			other.Actions = mergeAndDedupActions(other.Actions, s.Actions)
			return
		}
	}
	*ss = append(*ss, &s)
}

func mergeAndDedupActions(lhs []string, rhs []string) (result []string) {
	seen := make(map[string]bool)
	for _, elem := range append(lhs, rhs...) {
		if seen[elem] {
			continue
		}
		result = append(result, elem)
		seen[elem] = true
	}
	return
}
