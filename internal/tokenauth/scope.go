/******************************************************************************
*
*  Copyright 2018-2019 SAP SE
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

package tokenauth

import "strings"

//Scope contains the fields of the "scope" query parameter in a token request.
type Scope struct {
	ResourceType string   `json:"type"`
	ResourceName string   `json:"name"`
	Actions      []string `json:"actions"`
}

//AccountName returns the first path element of the resource name, if the
//resource type is "repository", or the empty string otherwise.
func (s Scope) AccountName() string {
	if s.ResourceType != "repository" {
		return ""
	}
	return strings.SplitN(s.ResourceName, "/", 2)[0]
}

//Contains returns true if this scope is for the same resource as the other
//scope, and if it contains all the actions that the other contains.
func (s Scope) Contains(other Scope) bool {
	if s.ResourceType != other.ResourceType {
		return false
	}
	if s.ResourceName != other.ResourceName {
		return false
	}
	actions := make(map[string]bool)
	for _, a := range s.Actions {
		actions[a] = true
	}
	for _, a := range other.Actions {
		if !actions[a] {
			return false
		}
	}
	return true
}

//String serializes this scope into the format used in the Docker auth API.
func (s Scope) String() string {
	return strings.Join([]string{
		s.ResourceType,
		s.ResourceName,
		strings.Join(s.Actions, ","),
	}, ":")
}
