/*******************************************************************************
*
* Copyright 2017-2018 SAP SE
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

package gopherpolicy

import (
	"fmt"
	"net/http"

	policy "github.com/databus23/goslo.policy"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
)

//Enforcer contains the Enforce method that struct Token requires to check
//access permissions. This interface is satisfied by struct Enforcer from
//goslo.policy.
type Enforcer interface {
	Enforce(rule string, c policy.Context) bool
}

//Token represents a validated Keystone v3 token. It is returned from
//Validator.CheckToken().
type Token struct {
	//The enforcer that checks access permissions for this client token. Usually
	//an instance of struct Enforcer from goslo.policy. Usually inherited from
	//struct TokenValidator.
	Enforcer Enforcer
	//When AuthN succeeds, contains information about the client token which can
	//be used to check access permissions.
	Context policy.Context
	//When AuthN succeeds, contains a fully-initialized ProviderClient with which
	//this process can use the OpenStack API on behalf of the authenticated user.
	ProviderClient *gophercloud.ProviderClient
	//When AuthN fails, contains the deferred AuthN error.
	Err error

	//When AuthN succeeds, contains all the information needed to serialize this
	//token in SerializeTokenForCache.
	serializable serializableToken
}

//Require checks if the given token has the given permission according to the
//policy.json that is in effect. If not, an error response is written and false
//is returned.
func (t *Token) Require(w http.ResponseWriter, rule string) bool {
	if t.Err != nil {
		if t.Context.Logger != nil {
			t.Context.Logger("returning 401 because of error: " + t.Err.Error())
		}
		http.Error(w, "Unauthorized", 401)
		return false
	}

	if !t.Enforcer.Enforce(rule, t.Context) {
		http.Error(w, "Forbidden", 403)
		return false
	}
	return true
}

//Check is like Require, but does not write error responses.
func (t *Token) Check(rule string) bool {
	return t.Err == nil && t.Enforcer.Enforce(rule, t.Context)
}

//UserUUID returns the UUID of the user for whom this token was issued, or ""
//if the token was invalid.
func (t *Token) UserUUID() string {
	return t.Context.Auth["user_id"]
}

//UserName returns the name of the user for whom this token was issued, or ""
//if the token was invalid.
func (t *Token) UserName() string {
	return t.Context.Auth["user_name"]
}

//UserDomainName returns the name of the domain containing the user for whom
//this token was issued, or "" if the token was invalid.
func (t *Token) UserDomainName() string {
	return t.Context.Auth["user_domain_name"]
}

//UserDomainUUID returns the UUID of the domain containing the user for whom
//this token was issued, or "" if the token was invalid.
func (t *Token) UserDomainUUID() string {
	return t.Context.Auth["user_domain_id"]
}

//ProjectScopeUUID returns the UUID of this token's project scope, or "" if the token is
//invalid or not scoped to a project.
func (t *Token) ProjectScopeUUID() string {
	return t.Context.Auth["project_id"]
}

//ProjectScopeName returns the name of this token's project scope, or "" if the token is
//invalid or not scoped to a project.
func (t *Token) ProjectScopeName() string {
	return t.Context.Auth["project_name"]
}

//ProjectScopeDomainUUID returns the UUID of this token's project scope domain, or ""
//if the token is invalid or not scoped to a project.
func (t *Token) ProjectScopeDomainUUID() string {
	return t.Context.Auth["project_domain_id"]
}

//ProjectScopeDomainName returns the name of this token's project scope domain, or ""
//if the token is invalid or not scoped to a project.
func (t *Token) ProjectScopeDomainName() string {
	return t.Context.Auth["project_domain_name"]
}

//DomainScopeUUID returns the UUID of this token's domain scope, or "" if the token is
//invalid or not scoped to a domain.
func (t *Token) DomainScopeUUID() string {
	return t.Context.Auth["domain_id"]
}

//DomainScopeName returns the name of this token's domain scope, or "" if the token is
//invalid or not scoped to a domain.
func (t *Token) DomainScopeName() string {
	return t.Context.Auth["domain_name"]
}

////////////////////////////////////////////////////////////////////////////////
// type serializableToken

type serializableToken struct {
	Token          tokens.Token          `json:"token_id"`
	TokenData      keystoneToken         `json:"token_data"`
	ServiceCatalog []tokens.CatalogEntry `json:"catalog"`
}

//ExtractInto implements the TokenResult interface.
func (s serializableToken) ExtractInto(value interface{}) error {
	//TokenResult.ExtractInto is only ever called with a value of type
	//*keystoneToken, so this is okay
	kd, ok := value.(*keystoneToken)
	if !ok {
		return fmt.Errorf("serializableToken.ExtractInto called with unsupported target type %T", value)
	}
	*kd = s.TokenData
	return nil
}

//Extract implements the TokenResult interface.
func (s serializableToken) Extract() (*tokens.Token, error) {
	return &s.Token, nil
}

//ExtractServiceCatalog implements the TokenResult interface.
func (s serializableToken) ExtractServiceCatalog() (*tokens.ServiceCatalog, error) {
	return &tokens.ServiceCatalog{Entries: s.ServiceCatalog}, nil
}
