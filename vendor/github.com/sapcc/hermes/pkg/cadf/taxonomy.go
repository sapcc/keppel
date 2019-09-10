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
