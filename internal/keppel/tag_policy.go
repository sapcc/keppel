// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0
package keppel

import (
	"encoding/json"
)

// TagPolicy is a policy describing what happens with tags
type TagPolicy struct {
	PolicyMatchRule
	BlockOverwrite bool `json:"block_overwrite,omitempty"`
	BlockDelete    bool `json:"block_delete,omitempty"`
	BlockPush      bool `json:"block_push,omitempty"`
}

// ParseTagPolicies parses the Tag policies for the given account.
func ParseTagPolicies(tagPoliciesJSON string) ([]TagPolicy, error) {
	if tagPoliciesJSON == "" || tagPoliciesJSON == "[]" {
		return nil, nil
	}
	var policies []TagPolicy
	err := json.Unmarshal([]byte(tagPoliciesJSON), &policies)
	return policies, err
}

func (t TagPolicy) Validate() error {
	return t.validate("tag policy")
}

func (t TagPolicy) String() string {
	b, err := json.Marshal(t)
	if err != nil {
		return "<error>"
	}
	return string(b)
}
