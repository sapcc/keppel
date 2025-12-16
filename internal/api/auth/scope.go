// SPDX-FileCopyrightText: 2018 SAP SE or an SAP affiliate company
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
		// This vaguely follows the implementars note from the spec:
		// https://github.com/opencontainers/distribution-spec/blob/main/spec.md#pulling-manifests:~:text=a%2Dz0%2D9%5D%2B)*)*-,Implementers%20note,-%3A%20Many%20clients
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
