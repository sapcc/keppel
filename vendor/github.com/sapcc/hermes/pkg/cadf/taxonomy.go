/*******************************************************************************
*
* Copyright 2022 SAP SE
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

package cadf

import (
	"strings"
)

// IsTypeURI that matches CADF Taxonomy. Full CADF Taxonomy
// available in the documentation. Match Prefix
func IsTypeURI(TypeURI string) bool {
	validTypeURIs := []string{"storage", "compute", "network", "data", "service"}

	for _, tu := range validTypeURIs {
		if strings.HasPrefix(TypeURI, tu) {
			return true
		}
	}
	return false
}

//IsAction validates a CADF Action: Exact match
func IsAction(Action string) bool {
	validActions := []string{
		"backup",
		"capture",
		"create",
		"configure",
		"read",
		"list",
		"update",
		"delete",
		"monitor",
		"start",
		"stop",
		"deploy",
		"undeploy",
		"enable",
		"disable",
		"send",
		"receive",
		"authenticate",
		"authenticate/login",
		"revoke",
		"renew",
		"restore",
		"evaluate",
		"allow",
		"deny",
		"notify",
		"unknown",
	}

	for _, a := range validActions {
		if Action == a {
			return true
		}
	}
	return false
}

//IsOutcome CADF Outcome: Exact Match
func IsOutcome(outcome string) bool {
	validOutcomes := []string{
		"success",
		"failure",
		"pending",
	}

	for _, o := range validOutcomes {
		if outcome == o {
			return true
		}
	}
	return false
}

//GetAction returns the Action for each http request method.
func GetAction(req string) (action string) {
	switch req {
	case "get":
		action = "read"
	case "head":
		action = "read"
	case "post":
		action = "create"
	case "put":
		action = "update"
	case "delete":
		action = "delete"
	case "patch":
		action = "update"
	case "options":
		action = "read"
	default:
		action = "unknown"
	}
	return action
}
