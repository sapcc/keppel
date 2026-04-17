// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"slices"

	"github.com/sapcc/keppel/internal/models"
)

// ScopeSet is a set of scopes.
type ScopeSet []Scope

// NewScopeSet initializes a ScopeSet.
func NewScopeSet(scopes ...Scope) (ss ScopeSet) {
	for _, s := range scopes {
		ss.Add(s)
	}
	return ss
}

// Contains returns true if the given token authorizes the user for this scope.
func (ss ScopeSet) Contains(s Scope) bool {
	for _, scope := range ss {
		if scope.Contains(s) {
			return true
		}
	}
	return false
}

// Add adds a scope to this ScopeSet. If the ScopeSet already contains a Scope
// referring to the same resource, it is merged with the given scope.
func (ss *ScopeSet) Add(s Scope) {
	if len(s.Actions) == 0 {
		return
	}
	for i, other := range *ss {
		if s.ResourceType == other.ResourceType && s.ResourceName == other.ResourceName {
			(*ss)[i].Actions = mergeAndDedupActions((*ss)[i].Actions, s.Actions)
			return
		}
	}
	*ss = append(*ss, s)
}

func mergeAndDedupActions(lhs, rhs []string) []string {
	seen := make(map[string]bool, len(lhs))
	for _, elem := range lhs {
		seen[elem] = true
	}

	for _, elem := range rhs {
		if seen[elem] {
			continue
		}
		lhs = append(lhs, elem)
		seen[elem] = true
	}
	return lhs
}

// AccountsWithCatalogAccess returns the names of all accounts whose contents
// can be listed with the access level in this ScopeSet. If `markerAccountName`
// is not empty, only accounts with `name > markerAccountName` will be returned.
//
// For use with the /v2/_catalog endpoint.
func (ss ScopeSet) AccountsWithCatalogAccess(markerAccountName models.AccountName) []models.AccountName {
	var result []models.AccountName
	for _, scope := range ss {
		accountName, ok := isKeppelAccountViewScope(scope)
		if !ok {
			continue
		}
		// when paginating, we don't need to care about accounts before the marker
		if markerAccountName == "" || accountName >= markerAccountName {
			result = append(result, accountName)
		}
	}
	return result
}

func isKeppelAccountViewScope(s Scope) (models.AccountName, bool) {
	if s.ResourceType != "keppel_account" {
		return "", false
	}
	if slices.Contains(s.Actions, "view") {
		return models.AccountName(s.ResourceName), true
	}
	return "", false
}
