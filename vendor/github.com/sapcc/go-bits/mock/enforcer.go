// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package mock

import policy "github.com/databus23/goslo.policy"

// Enforcer implements the gopherpolicy.Enforcer interface. During enforcement,
// all accesses are allowed by default. More restrictive policies can be
// configured with Forbid() and Allow(). Request attributes cannot be checked.
type Enforcer struct {
	forbiddenRules map[string]bool
}

// NewEnforcer initializes an Enforcer instance.
func NewEnforcer() *Enforcer {
	return &Enforcer{make(map[string]bool)}
}

// Forbid will cause all subsequent calls to Enforce() to return false when
// called for this rule.
func (e *Enforcer) Forbid(rule string) {
	e.forbiddenRules[rule] = true
}

// Allow reverses a previous Forbid call and allows the given policy rule.
func (e *Enforcer) Allow(rule string) {
	e.forbiddenRules[rule] = false
}

// Enforce implements the gopherpolicy.Enforcer interface.
func (e *Enforcer) Enforce(rule string, ctx policy.Context) bool {
	return !e.forbiddenRules[rule]
}
