// SPDX-FileCopyrightText: 2018 SAP SE
// SPDX-License-Identifier: Apache-2.0

package authapi

import (
	"strings"

	"github.com/sapcc/go-bits/logg"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/models"
)

func parseScope(input string) auth.Scope {
	fields := strings.SplitN(input, ":", 3)
	scope := auth.Scope{
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
		} else if !models.RepoPathRx.MatchString(scope.ResourceName) {
			logg.Info("rejecting invalid repository name: %q", scope.ResourceName)
			scope.ResourceName = ""
		}
	}
	return scope
}

func parseScopes(inputs []string) auth.ScopeSet {
	var ss auth.ScopeSet
	for _, input := range inputs {
		ss.Add(parseScope(input))
	}
	return ss
}
