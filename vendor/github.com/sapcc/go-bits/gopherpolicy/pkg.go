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

//Package gopherpolicy provides integration between goslo.policy and
//Gophercloud for services that need to validate OpenStack tokens and check permissions.
package gopherpolicy

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"

	policy "github.com/databus23/goslo.policy"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
)

//Validator is the interface provided by TokenValidator. Application code
//should prefer to reference this interface to allow for substituation by a
//test double.
type Validator interface {
	//CheckToken checks the validity of the request's X-Auth-Token in Keystone, and
	//returns a Token instance for checking authorization. Any errors that occur
	//during this function are deferred until Token.Require() is called.
	CheckToken(r *http.Request) *Token
}

//TokenValidator combines an Identity v3 client to validate tokens (AuthN), and
//a policy.Enforcer to check access permissions (AuthZ).
type TokenValidator struct {
	IdentityV3 *gophercloud.ServiceClient
	//Enforcer can also be initialized with the LoadPolicyFile method.
	Enforcer Enforcer
}

//LoadPolicyFile creates v.Enforcer from the given policy file.
func (v *TokenValidator) LoadPolicyFile(path string) error {
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	var rules map[string]string
	err = json.Unmarshal(bytes, &rules)
	if err != nil {
		return err
	}
	v.Enforcer, err = policy.NewEnforcer(rules)
	return err
}

//CheckToken checks the validity of the request's X-Auth-Token in Keystone, and
//returns a Token instance for checking authorization. Any errors that occur
//during this function are deferred until Require() is called.
func (v *TokenValidator) CheckToken(r *http.Request) *Token {
	str := r.Header.Get("X-Auth-Token")
	if str == "" {
		return &Token{Err: errors.New("X-Auth-Token header missing")}
	}

	response := tokens.Get(v.IdentityV3, str)
	if response.Err != nil {
		//this includes 4xx responses, so after this point, we can be sure that the token is valid
		return &Token{Err: response.Err}
	}

	return v.TokenFromGophercloudResult(response)
}

//TokenFromGophercloudResult creates a Token instance from a gophercloud Result
//from the tokens.Create() or tokens.Get() requests from package
//github.com/gophercloud/gophercloud/openstack/identity/v3/tokens.
func (v *TokenValidator) TokenFromGophercloudResult(result TokenResult) *Token {
	//use a custom token struct instead of tokens.Token which is way incomplete
	var tokenData keystoneToken
	err := result.ExtractInto(&tokenData)
	if err != nil {
		return &Token{Err: err}
	}
	token, err := result.Extract()
	if err != nil {
		return &Token{Err: err}
	}
	catalog, err := result.ExtractServiceCatalog()
	if err != nil {
		return &Token{Err: err}
	}

	return &Token{
		Enforcer: v.Enforcer,
		Context:  tokenData.ToContext(),
		ProviderClient: &gophercloud.ProviderClient{
			IdentityBase:     v.IdentityV3.ProviderClient.IdentityBase,
			IdentityEndpoint: v.IdentityV3.ProviderClient.IdentityEndpoint,
			HTTPClient:       v.IdentityV3.ProviderClient.HTTPClient,
			UserAgent:        v.IdentityV3.ProviderClient.UserAgent,
			TokenID:          token.ID,
			EndpointLocator: func(opts gophercloud.EndpointOpts) (string, error) {
				return openstack.V3EndpointURL(catalog, opts)
			},
		},
	}
}

//TokenResult is the interface type for the argument of
//TokenValidator.TokenFromGophercloudResult().
type TokenResult interface {
	ExtractInto(value interface{}) error
	Extract() (*tokens.Token, error)
	ExtractServiceCatalog() (*tokens.ServiceCatalog, error)
}

type keystoneToken struct {
	DomainScope  keystoneTokenThing         `json:"domain"`
	ProjectScope keystoneTokenThingInDomain `json:"project"`
	Roles        []keystoneTokenThing       `json:"roles"`
	User         keystoneTokenThingInDomain `json:"user"`
}

type keystoneTokenThing struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type keystoneTokenThingInDomain struct {
	keystoneTokenThing
	Domain keystoneTokenThing `json:"domain"`
}

func (t *keystoneToken) ToContext() policy.Context {
	c := policy.Context{
		Roles: make([]string, 0, len(t.Roles)),
		Auth: map[string]string{
			"user_id":             t.User.ID,
			"user_name":           t.User.Name,
			"user_domain_id":      t.User.Domain.ID,
			"user_domain_name":    t.User.Domain.Name,
			"domain_id":           t.DomainScope.ID,
			"domain_name":         t.DomainScope.Name,
			"project_id":          t.ProjectScope.ID,
			"project_name":        t.ProjectScope.Name,
			"project_domain_id":   t.ProjectScope.Domain.ID,
			"project_domain_name": t.ProjectScope.Domain.Name,
			"tenant_id":           t.ProjectScope.ID,
			"tenant_name":         t.ProjectScope.Name,
			"tenant_domain_id":    t.ProjectScope.Domain.ID,
			"tenant_domain_name":  t.ProjectScope.Domain.Name,
		},
		Request: nil,
	}
	for key, value := range c.Auth {
		if value == "" {
			delete(c.Auth, key)
		}
	}
	for _, role := range t.Roles {
		c.Roles = append(c.Roles, role.Name)
	}
	if c.Request == nil {
		c.Request = map[string]string{}
	}

	return c
}
