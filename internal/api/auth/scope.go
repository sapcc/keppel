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

package authapi

import (
	"strings"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/tokenauth"
)

func parseScope(input string) tokenauth.Scope {
	fields := strings.SplitN(input, ":", 3)
	scope := tokenauth.Scope{
		ResourceType: fields[0],
	}
	if len(fields) > 1 {
		scope.ResourceName = fields[1]
	}
	if len(fields) > 2 {
		scope.Actions = strings.Split(fields[2], ",")
	}

	if scope.ResourceType == "repository" {
		if len(scope.ResourceName) > 256 {
			logg.Info("rejecting overlong repository name: %q", scope.ResourceName)
			scope.ResourceName = ""
		} else if !keppel.RepoPathRx.MatchString(scope.ResourceName) {
			logg.Info("rejecting invalid repository name: %q", scope.ResourceName)
			scope.ResourceName = ""
		}
	}
	return scope
}

func parseScopes(inputs []string) tokenauth.ScopeSet {
	var ss tokenauth.ScopeSet
	for _, input := range inputs {
		ss.Add(parseScope(input))
	}
	return ss
}
