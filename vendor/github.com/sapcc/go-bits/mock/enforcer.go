/******************************************************************************
*
*  Copyright 2023 SAP SE
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
