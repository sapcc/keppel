/*******************************************************************************
*
* Copyright 2021 SAP SE
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

package auth

import (
	"fmt"
	"time"

	"github.com/sapcc/go-bits/audittools"

	"github.com/sapcc/keppel/internal/keppel"
)

func init() {
	keppel.UserIdentityRegistry.Add(func() keppel.UserIdentity { return &TrivyUserIdentity{} })
}

// TrivyUserIdentity is a keppel.UserIdentity for peer users with global read
// access and access to the specialized peer API.
type TrivyUserIdentity struct{}

// UserType implements the keppel.UserIdentity interface.
func (uid *TrivyUserIdentity) PluginTypeID() string {
	return "trivy"
}

// HasPermission implements the keppel.UserIdentity interface.
func (uid *TrivyUserIdentity) HasPermission(perm keppel.Permission, tenantID string) bool {
	// allow universal pull access for security scanning purposes
	return perm == keppel.CanViewAccount || perm == keppel.CanPullFromAccount
}

// UserType implements the keppel.UserIdentity interface.
func (uid *TrivyUserIdentity) UserType() keppel.UserType {
	return keppel.TrivyUser
}

// UserName implements the keppel.UserIdentity interface.
func (uid *TrivyUserIdentity) UserName() string {
	return "trivy"
}

// UserInfo implements the keppel.UserIdentity interface.
func (uid *TrivyUserIdentity) UserInfo() audittools.UserInfo {
	return nil
}

// SerializeToJSON implements the keppel.UserIdentity interface.
func (uid *TrivyUserIdentity) SerializeToJSON() (payload []byte, err error) {
	return []byte("true"), nil
}

// DeserializeFromJSON implements the keppel.UserIdentity interface.
func (uid *TrivyUserIdentity) DeserializeFromJSON(in []byte, _ keppel.AuthDriver) error {
	if string(in) != "true" {
		return fmt.Errorf("%q is not a valid payload for TrivyUserIdentity", string(in))
	}
	return nil
}

// IssueTokenForTrivy issues a token for Trivy to pull the image and it's databases with.
// This needs to use the specialized TrivyUserIdentity to avoid updating the image's "last_pulled_at" timestamp.
func IssueTokenForTrivy(cfg keppel.Configuration, repoFullName string) (*TokenResponse, error) {
	scopes := []Scope{{
		ResourceType: "repository",
		ResourceName: repoFullName,
		Actions:      []string{"pull"},
	}}

	for _, repo := range cfg.Trivy.AdditionalPullableRepos {
		scopes = append(scopes, Scope{
			ResourceType: "repository",
			ResourceName: repo,
			Actions:      []string{"pull"},
		})
	}

	return Authorization{
		UserIdentity: &TrivyUserIdentity{},
		Audience:     Audience{},
		ScopeSet:     NewScopeSet(scopes...),
	}.IssueTokenWithExpires(cfg, 20*time.Minute)
}
