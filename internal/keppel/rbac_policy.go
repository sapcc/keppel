// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/sapcc/go-bits/regexpext"
	. "go.xyrillian.de/gg/option"

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

// ValidateAndNormalize performs some normalizations and returns an error if this policy is invalid.
// On success, if the policy governs access for anonymous users, the respective [AnonymousRBACPolicy] is returned.
// Otherwise, if the policy governs access for authenticated users, [None] is returned.
//
// [None]: https://pkg.go.dev/go.xyrillian.de/gg/option#None
func (r *RBACPolicy) ValidateAndNormalize(strategy ReplicationStrategy) (Option[AnonymousRBACPolicy], error) {
	var none Option[AnonymousRBACPolicy] // for use in error returns

	if r.CidrPattern != "" {
		_, network, err := net.ParseCIDR(r.CidrPattern)
		if err != nil {
			// err.Error() sadly does not contain any useful information why the cidr is invalid
			return none, fmt.Errorf("%q is not a valid CIDR", r.CidrPattern)
		}
		r.CidrPattern = network.String()
		if network.String() == "0.0.0.0/0" {
			return none, errors.New("0.0.0.0/0 cannot be used as CIDR because it matches everything")
		}
	}

	grantsPerm := make(map[RBACPermission]bool)   // set of permissions named in `r.Permissions`
	forbidsPerm := make(map[RBACPermission]bool)  // set of permissions named in `r.NegativePermissions`
	refersToPerm := make(map[RBACPermission]bool) // set of permissions named in either `r.Permissions` or `r.NegativePermissions`
	for _, perm := range r.Permissions {
		if !isRBACPermission[perm] {
			return none, fmt.Errorf("%q is not a valid RBAC policy permission", perm)
		}
		grantsPerm[perm] = true
		forbidsPerm[perm] = false
		refersToPerm[perm] = true
	}
	for _, perm := range r.ForbiddenPermissions {
		if !isRBACPermission[perm] {
			return none, fmt.Errorf("%q is not a valid RBAC policy permission", perm)
		}
		if grantsPerm[perm] {
			return none, fmt.Errorf("%q cannot be granted and forbidden by the same RBAC policy", perm)
		}
		grantsPerm[perm] = false
		forbidsPerm[perm] = true
		refersToPerm[perm] = true
	}

	if len(r.Permissions) == 0 && len(r.ForbiddenPermissions) == 0 {
		return none, errors.New(`RBAC policy must grant at least one permission`)
	}
	if r.CidrPattern == "" && r.UserNamePattern == "" && r.RepositoryPattern == "" {
		return none, errors.New(`RBAC policy must have at least one "match_..." attribute`)
	}
	if (refersToPerm[RBACAnonymousPullPermission] || refersToPerm[RBACAnonymousFirstPullPermission]) && r.UserNamePattern != "" {
		return none, errors.New(`RBAC policy with "anonymous_pull" or "anonymous_first_pull" may not have the "match_username" attribute`)
	}
	if refersToPerm[RBACPullPermission] && r.UserNamePattern == "" {
		return none, errors.New(`RBAC policy with "pull" must have the "match_username" attribute`)
	}
	if grantsPerm[RBACPushPermission] && !grantsPerm[RBACPullPermission] {
		return none, errors.New(`RBAC policy with "push" must also grant "pull"`)
	}
	if grantsPerm[RBACAnonymousFirstPullPermission] && !grantsPerm[RBACAnonymousPullPermission] {
		return none, errors.New(`RBAC policy with "anonymous_first_pull" must also grant "anonymous_pull"`)
	}
	if refersToPerm[RBACDeletePermission] && r.UserNamePattern == "" {
		return none, errors.New(`RBAC policy with "delete" must have the "match_username" attribute`)
	}
	if refersToPerm[RBACAnonymousFirstPullPermission] && strategy == NoReplicationStrategy {
		return none, errors.New(`RBAC policy with "anonymous_first_pull" may only be for replica accounts`)
	}

	if len(r.Permissions) == 0 {
		// the "permissions" field is not documented as optional, so `null` values should be avoided and empty lists should only be represented as `[]`
		r.Permissions = []RBACPermission{}
	}

	if r.UserNamePattern == "" {
		return Some(AnonymousRBACPolicy{
			cidrPattern:       r.CidrPattern,
			repositoryPattern: string(r.RepositoryPattern),
			grantsPull:        grantsPerm[RBACAnonymousPullPermission],
			forbidsPull:       forbidsPerm[RBACAnonymousPullPermission],
			grantsFirstPull:   grantsPerm[RBACAnonymousFirstPullPermission],
			forbidsFirstPull:  forbidsPerm[RBACAnonymousFirstPullPermission],
		}), nil
	} else {
		return None[AnonymousRBACPolicy](), nil
	}
}

// ParseRBACPolicies parses the RBAC policies for the given account.
func ParseRBACPolicies(account models.Account) ([]RBACPolicy, error) {
	return ParseRBACPoliciesField([]byte(account.RBACPoliciesJSON))
}

// ParseRBACPoliciesField is like ParseRBACPolicies, but only takes the
// RBACPoliciesJSON field of type Account instead of the whole Account.
//
// This is useful when the full Account has not been loaded from the DB.
func ParseRBACPoliciesField(buf []byte) ([]RBACPolicy, error) {
	if len(buf) == 0 || bytes.Equal(buf, []byte("[]")) {
		return nil, nil
	}
	var policies []RBACPolicy
	err := json.Unmarshal(buf, &policies)
	return policies, err
}

// AnonymousRBACPolicy is a trimmed-down version of [RBACPolicy] that only covers access control for anonymous users:
//
//   - Policies matching on user name cannot be converted into this format.
//   - Policies granting permissions other than [RBACAnonymousPullPermission] and [RBACAnonymousFirstPullPermission] cannot be converted into this format.
//
// When serialized into JSON, this type yields an extremely compact encoding.
// Anonymous RBAC policies are meant for reading from the DB even during extremely hot paths,
// if doing so can avoid issuing tokens with cryptographic signatures and incurring the performance penalty of verifying these signatures.
type AnonymousRBACPolicy struct {
	cidrPattern       string
	repositoryPattern string
	grantsPull        bool
	grantsFirstPull   bool
	forbidsPull       bool
	forbidsFirstPull  bool
}

// serializedAnonymousRBACPolicy defines how [AnonymousRBACPolicy] gets serialized as JSON.
type serializedAnonymousRBACPolicy struct {
	CidrPattern       string `json:"c,omitempty"`
	RepositoryPattern string `json:"r,omitempty"`
	Permissions       string `json:"p"`
}

// MarshalJSONTo implements the [json.MarshalerTo] interface.
func (a AnonymousRBACPolicy) MarshalJSONTo(enc *jsontext.Encoder) error {
	var perms []string
	if a.grantsPull {
		perms = append(perms, "p")
	}
	if a.forbidsPull {
		perms = append(perms, "!p")
	}
	if a.grantsFirstPull {
		perms = append(perms, "f")
	}
	if a.forbidsFirstPull {
		perms = append(perms, "!f")
	}
	return json.MarshalEncode(enc, serializedAnonymousRBACPolicy{
		CidrPattern:       a.cidrPattern,
		RepositoryPattern: a.repositoryPattern,
		Permissions:       strings.Join(perms, ","),
	})
}

// UnmarshalJSONFrom implements the [json.UnmarshalerForm] interface.
func (a *AnonymousRBACPolicy) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var s serializedAnonymousRBACPolicy
	err := json.UnmarshalDecode(dec, &s)
	if err != nil {
		return err
	}

	*a = AnonymousRBACPolicy{
		cidrPattern:       s.CidrPattern,
		repositoryPattern: s.RepositoryPattern,
	}
	for perm := range strings.SplitSeq(s.Permissions, ",") {
		switch perm {
		case "p":
			a.grantsPull = true
		case "!p":
			a.forbidsPull = true
		case "f":
			a.grantsFirstPull = true
		case "!f":
			a.forbidsFirstPull = true
		default:
			return &json.SemanticError{Err: fmt.Errorf("invalid permission code: %q", perm)}
		}
	}
	return nil
}
