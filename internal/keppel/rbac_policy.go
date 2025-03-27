/*******************************************************************************
*
* Copyright 2024 SAP SE
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

package keppel

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"

	"github.com/sapcc/go-bits/regexpext"

	"github.com/sapcc/keppel/internal/models"
)

// RBACPolicy is a policy granting user-defined access to repos in an account.
// It is stored in serialized form in the RBACPoliciesJSON field of type Account.
type RBACPolicy struct {
	CidrPattern          string                  `json:"match_cidr,omitempty"`
	RepositoryPattern    regexpext.BoundedRegexp `json:"match_repository,omitempty"`
	UserNamePattern      regexpext.BoundedRegexp `json:"match_username,omitempty"`
	Permissions          []RBACPermission        `json:"permissions"`
	ForbiddenPermissions []RBACPermission        `json:"forbidden_permissions,omitempty"`
}

// RBACPermission enumerates permissions that can be granted by an RBAC policy.
type RBACPermission string

const (
	RBACPullPermission               RBACPermission = "pull"
	RBACPushPermission               RBACPermission = "push"
	RBACDeletePermission             RBACPermission = "delete"
	RBACAnonymousPullPermission      RBACPermission = "anonymous_pull"
	RBACAnonymousFirstPullPermission RBACPermission = "anonymous_first_pull"
)

var isRBACPermission = map[RBACPermission]bool{
	RBACPullPermission:               true,
	RBACPushPermission:               true,
	RBACDeletePermission:             true,
	RBACAnonymousPullPermission:      true,
	RBACAnonymousFirstPullPermission: true,
}

// Matches evaluates the cidr and regexes in this policy.
func (r RBACPolicy) Matches(ip, repoName, userName string) bool {
	if r.CidrPattern != "" {
		ip := net.ParseIP(ip)
		_, network, err := net.ParseCIDR(r.CidrPattern)
		if err != nil || !network.Contains(ip) {
			return false
		}
	}

	if r.RepositoryPattern != "" && !r.RepositoryPattern.MatchString(repoName) {
		return false
	}
	if r.UserNamePattern != "" && !r.UserNamePattern.MatchString(userName) {
		return false
	}

	return true
}

// ValidateAndNormalize performs some normalizations and returns an error if
// this policy is invalid.
func (r *RBACPolicy) ValidateAndNormalize(strategy ReplicationStrategy) error {
	if r.CidrPattern != "" {
		_, network, err := net.ParseCIDR(r.CidrPattern)
		if err != nil {
			// err.Error() sadly does not contain any useful information why the cidr is invalid
			return fmt.Errorf("%q is not a valid CIDR", r.CidrPattern)
		}
		r.CidrPattern = network.String()
		if network.String() == "0.0.0.0/0" {
			return errors.New("0.0.0.0/0 cannot be used as CIDR because it matches everything")
		}
	}

	grantsPerm := make(map[RBACPermission]bool)
	forbidsPerm := make(map[RBACPermission]bool)
	refersToPerm := make(map[RBACPermission]bool)
	for _, perm := range r.Permissions {
		if !isRBACPermission[perm] {
			return fmt.Errorf("%q is not a valid RBAC policy permission", perm)
		}
		grantsPerm[perm] = true
		forbidsPerm[perm] = false
		refersToPerm[perm] = true
	}
	for _, perm := range r.ForbiddenPermissions {
		if !isRBACPermission[perm] {
			return fmt.Errorf("%q is not a valid RBAC policy permission", perm)
		}
		if grantsPerm[perm] {
			return fmt.Errorf("%q cannot be granted and forbidden by the same RBAC policy", perm)
		}
		grantsPerm[perm] = false
		forbidsPerm[perm] = true
		refersToPerm[perm] = true
	}

	if len(r.Permissions) == 0 && len(r.ForbiddenPermissions) == 0 {
		return errors.New(`RBAC policy must grant at least one permission`)
	}
	if r.CidrPattern == "" && r.UserNamePattern == "" && r.RepositoryPattern == "" {
		return errors.New(`RBAC policy must have at least one "match_..." attribute`)
	}
	if (refersToPerm[RBACAnonymousPullPermission] || refersToPerm[RBACAnonymousFirstPullPermission]) && r.UserNamePattern != "" {
		return errors.New(`RBAC policy with "anonymous_pull" or "anonymous_first_pull" may not have the "match_username" attribute`)
	}
	if refersToPerm[RBACPullPermission] && r.CidrPattern == "" && r.UserNamePattern == "" {
		return errors.New(`RBAC policy with "pull" must have the "match_cidr" or "match_username" attribute`)
	}
	if grantsPerm[RBACPushPermission] && !grantsPerm[RBACPullPermission] {
		return errors.New(`RBAC policy with "push" must also grant "pull"`)
	}
	if refersToPerm[RBACDeletePermission] && r.UserNamePattern == "" {
		return errors.New(`RBAC policy with "delete" must have the "match_username" attribute`)
	}
	if refersToPerm[RBACAnonymousFirstPullPermission] && strategy != FromExternalOnFirstUseStrategy {
		return errors.New(`RBAC policy with "anonymous_first_pull" may only be for external replica accounts`)
	}

	return nil
}

// ParseRBACPolicies parses the RBAC policies for the given account.
func ParseRBACPolicies(account models.Account) ([]RBACPolicy, error) {
	return ParseRBACPoliciesField(account.RBACPoliciesJSON)
}

// ParseRBACPoliciesField is like ParseRBACPolicies, but only takes the
// RBACPoliciesJSON field of type Account instead of the whole Account.
//
// This is useful when the full Account has not been loaded from the DB.
func ParseRBACPoliciesField(buf string) ([]RBACPolicy, error) {
	if buf == "" || buf == "[]" {
		return nil, nil
	}
	var policies []RBACPolicy
	err := json.Unmarshal([]byte(buf), &policies)
	return policies, err
}
