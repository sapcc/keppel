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

package auth

import (
	"fmt"
	"strings"
)

// Scope contains the fields of the "scope" query parameter in a token request.
type Scope struct {
	ResourceType string   `json:"type"`
	ResourceName string   `json:"name"`
	Actions      []string `json:"actions"`
}

// ParsedRepositoryScope is returned by Scope.ParseRepositoryScope().
type ParsedRepositoryScope struct {
	AccountName        string
	RepositoryName     string
	FullRepositoryName string
}

// ParseRepositoryScope interprets the resource name of a scope with resource
// type "repository".
//
// This is more complicated than it would appear in first sight since the
// audience plays a role in how repository scopes look: On the regular APIs, the
// scope "repository:foo/bar:pull" refers to the repository "bar" in the account
// "foo". On the domain-remapped API for that account, the same repository would
// be accessed with the scope "repository:bar:pull".
//
// I'm well aware that everything would be much easier if we used
// "repository:foo/bar:pull" all the time, but our hand is being forced by the
// Docker client here. It auto-guesses repository scopes based on the repository
// URL, which for domain-remapped APIs only has the repository name in the URL
// path.
func (s Scope) ParseRepositoryScope(audience Audience) ParsedRepositoryScope {
	if s.ResourceType != "repository" {
		return ParsedRepositoryScope{}
	}

	if audience.AccountName != "" {
		return ParsedRepositoryScope{
			AccountName:        audience.AccountName,
			RepositoryName:     s.ResourceName,
			FullRepositoryName: fmt.Sprintf("%s/%s", audience.AccountName, s.ResourceName),
		}
	}

	parts := strings.SplitN(s.ResourceName, "/", 2)
	if len(parts) == 1 {
		// we're on a non-domain-remapped API, but there is no "/" in the full
		// repository name, i.e. we have an account name without a corresponding
		// repository name which is not allowed; generate a ParsedRepositoryScope
		// that will never have any permissions given out for it
		return ParsedRepositoryScope{
			AccountName:        s.ResourceName,
			RepositoryName:     "",
			FullRepositoryName: s.ResourceName,
		}
	}
	return ParsedRepositoryScope{
		AccountName:        parts[0],
		RepositoryName:     parts[1],
		FullRepositoryName: s.ResourceName,
	}
}

// Contains returns true if this scope is for the same resource as the other
// scope, and if it contains all the actions that the other contains.
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

// String serializes this scope into the format used in the Docker auth API.
func (s Scope) String() string {
	return strings.Join([]string{
		s.ResourceType,
		s.ResourceName,
		strings.Join(s.Actions, ","),
	}, ":")
}

////////////////////////////////////////////////////////////////////////////////
// predefined scopes

// CatalogEndpointScope is the Scope for `GET /v2/_catalog`.
var CatalogEndpointScope = Scope{
	ResourceType: "registry",
	ResourceName: "catalog",
	Actions:      []string{"*"},
}

// PeerAPIScope is the Scope for all endpoints below `/peer/`.
var PeerAPIScope = Scope{
	ResourceType: "keppel_api",
	ResourceName: "peer",
	Actions:      []string{"access"},
}

// InfoAPIScope is the Scope for all informational endpoints that are allowed for all non-anon users.
var InfoAPIScope = Scope{
	ResourceType: "keppel_api",
	ResourceName: "info",
	Actions:      []string{"access"},
}
