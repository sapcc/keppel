/*******************************************************************************
*
* Copyright 2018 SAP SE
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

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	repoComponentRegexp = `[a-z0-9]+(?:[._-][a-z0-9]+)*`
	repoNameRegexp      = regexp.MustCompile(`^` + repoComponentRegexp + `(?:/` + repoComponentRegexp + `)*$`)

	errorScopeMissing             = errors.New("scope is missing")
	errorScopeMissingResource     = errors.New("scope is missing a resource")
	errorScopeMissingRepository   = errors.New("scope is missing a repository")
	errorScopeMissingActions      = errors.New("scope is missing actions")
	errorScopeInvalid             = errors.New("scope is invalid")
	errorScopeResourceUnsupported = errors.New("resource is unsupported")
	errorScopeRepositoryTooLong   = errors.New("repository must be less than 256 characters long")
	errorScopeRepositoryInvalid   = fmt.Errorf("repository name must match %q", repoNameRegexp.String())
	errorScopeActionUndefined     = errors.New("actions must not be empty")
	errorScopeActionInvalid       = errors.New("actions contains invalid value")
)

//Scope contains the fields of the "scope" query parameter in a token request.
type Scope struct {
	ResourceType string   `json:"type"`
	ResourceName string   `json:"name"`
	Actions      []string `json:"actions"`
}

//ParseScope parses the "scope" query parameter from a token request.
//
//	scope, err := auth.ParseScope(r.URL.Query()["scope"])
func ParseScope(input string) (Scope, error) {
	if input == "" {
		return Scope{}, errorScopeMissing
	}

	fields := strings.Split(input, ":")
	if fields[0] == "" {
		return Scope{}, errorScopeMissingResource
	}
	if len(fields) > 3 {
		return Scope{}, errorScopeInvalid
	}
	if len(fields) == 2 {
		return Scope{}, errorScopeMissingActions
	}
	if len(fields) == 1 {
		return Scope{}, errorScopeMissingRepository
	}

	scope := Scope{
		ResourceType: fields[0],
		ResourceName: fields[1],
		Actions:      strings.Split(fields[2], ","),
	}
	if len(scope.Actions) == 0 {
		return Scope{}, errorScopeActionInvalid
	}

	switch scope.ResourceType {
	case "registry":
		if scope.ResourceName != "catalog" {
			return Scope{}, errorScopeResourceUnsupported
		}
		scope.Actions = []string{"*"}
	case "repository":
		if len(scope.ResourceName) > 256 {
			return Scope{}, errorScopeRepositoryTooLong
		}
		if !repoNameRegexp.MatchString(scope.ResourceName) {
			return Scope{}, errorScopeRepositoryInvalid
		}
		for _, action := range scope.Actions {
			if action != "pull" && action != "push" {
				return Scope{}, errorScopeActionInvalid
			}
		}
	default:
		return Scope{}, errorScopeResourceUnsupported
	}

	return scope, nil
}

//MustParseScope is like ParseScope, but panics on error.
func MustParseScope(input string) Scope {
	s, err := ParseScope(input)
	if err != nil {
		panic(err.Error())
	}
	return s
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
