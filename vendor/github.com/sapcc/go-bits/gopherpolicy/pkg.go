// SPDX-FileCopyrightText: 2017-2018 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

// Package gopherpolicy provides integration between goslo.policy and
// Gophercloud for services that need to validate OpenStack tokens and check permissions.
package gopherpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	policy "github.com/databus23/goslo.policy"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"

	"github.com/sapcc/go-bits/logg"
)

// Validator is the interface provided by TokenValidator. Application code
// should prefer to reference this interface to allow for substituation by a
// test double (such as type mock.Validator).
type Validator interface {
	// CheckToken checks the validity of the request's X-Auth-Token in Keystone, and
	// returns a Token instance for checking authorization. Any errors that occur
	// during this function are deferred until Token.Require() is called.
	CheckToken(r *http.Request) *Token
}

// Cacher is the generic interface for a token cache.
type Cacher interface {
	// StoreTokenPayload attempts to store the token payload corresponding to the
	// given credentials in the cache. Implementations shall treat `credentials`
	// as an opaque string and only use it as a cache key.
	StoreTokenPayload(ctx context.Context, credentials string, payload []byte)
	// LoadTokenPayload attempts to retrieve the payload for the given credentials
	// from the cache. If there nothing cached for these credentials, or if the
	// retrieval fails, nil shall be returned.
	LoadTokenPayload(ctx context.Context, credentials string) []byte
}

// TokenValidator combines an Identity v3 client to validate tokens (AuthN), and
// a policy.Enforcer to check access permissions (AuthZ).
type TokenValidator struct {
	IdentityV3 *gophercloud.ServiceClient
	// Enforcer can also be initialized with the LoadPolicyFile method.
	Enforcer Enforcer
	// Cacher can be used to cache validated tokens.
	Cacher Cacher
}

// LoadPolicyFile creates v.Enforcer from the given policy file.
//
// The second argument must be set to `yaml.Unmarshal` if you want to support
// policy.yaml files. This explicit dependency injection slot allows you to choose
// whether to use gopkg.in/yaml.v2 or gopkg.in/yaml.v3 or anything else.
//
// If `yamlUnmarshal` is given as nil, `json.Unmarshal` from the standard
// library will be used, so only policy.json files will be understood.
func (v *TokenValidator) LoadPolicyFile(path string, yamlUnmarshal func(in []byte, out any) error) error {
	unmarshal := yamlUnmarshal
	if yamlUnmarshal == nil {
		unmarshal = json.Unmarshal
		if strings.HasSuffix(path, ".yaml") {
			return fmt.Errorf("LoadPolicyFile cannot parse %s because YAML support is not available", path)
		}
	}

	bytes, err := os.ReadFile(path)
	if err != nil {
		return err // no fmt.Errorf() necessary, errors from package os are already very descriptive
	}
	var rules map[string]string
	err = unmarshal(bytes, &rules)
	if err != nil {
		return fmt.Errorf("while parsing structure of %s: %w", path, err)
	}
	v.Enforcer, err = policy.NewEnforcer(rules)
	if err != nil {
		return fmt.Errorf("while parsing policy rules found in %s: %w", path, err)
	}
	return nil
}

// CheckToken checks the validity of the request's X-Auth-Token in Keystone, and
// returns a Token instance for checking authorization. Any errors that occur
// during this function are deferred until Require() is called.
func (v *TokenValidator) CheckToken(r *http.Request) *Token {
	tokenStr := r.Header.Get("X-Auth-Token")
	if tokenStr == "" {
		return &Token{Err: errors.New("X-Auth-Token header missing")}
	}

	token := v.CheckCredentials(r.Context(), tokenStr, func() TokenResult {
		return tokens.Get(r.Context(), v.IdentityV3, tokenStr)
	})
	token.Context.Logger = logg.Debug
	logg.Debug("token has auth = %v", token.Context.Auth)
	logg.Debug("token has roles = %v", token.Context.Roles)
	return token
}

// CheckCredentials is a more generic version of CheckToken that can also be
// used when the user supplies credentials instead of a Keystone token.
//
// The `check` argument contains the logic for actually checking the user's
// credentials, usually by calling tokens.Create() or tokens.Get() from package
// github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens.
//
// The `cacheKey` argument shall be a string that identifies the given
// credentials. This key is used for caching the TokenResult in `v.Cacher` if
// that is non-nil.
func (v *TokenValidator) CheckCredentials(ctx context.Context, cacheKey string, check func() TokenResult) *Token {
	// prefer cached token payload over actually talking to Keystone (but fallback
	// to Keystone if the token payload deserialization fails)
	if v.Cacher != nil {
		payload := v.Cacher.LoadTokenPayload(ctx, cacheKey)
		if payload != nil {
			var s serializableToken
			err := json.Unmarshal(payload, &s)
			if err == nil && s.Token.ExpiresAt.After(time.Now()) {
				t := v.TokenFromGophercloudResult(s)
				if t.Err == nil {
					return t
				}
			}
		}
	}

	t := v.TokenFromGophercloudResult(check())

	// cache token payload if valid
	if t.Err == nil && v.Cacher != nil {
		payload, err := json.Marshal(t.serializable)
		if err == nil {
			v.Cacher.StoreTokenPayload(ctx, cacheKey, payload)
		}
	}

	return t
}

// TokenFromGophercloudResult creates a Token instance from a gophercloud Result
// from the tokens.Create() or tokens.Get() requests from package
// github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens.
func (v *TokenValidator) TokenFromGophercloudResult(result TokenResult) *Token {
	// use a custom token struct instead of tokens.Token which is way incomplete
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
			IdentityBase:     v.IdentityV3.IdentityBase,
			IdentityEndpoint: v.IdentityV3.IdentityEndpoint,
			HTTPClient:       v.IdentityV3.HTTPClient,
			UserAgent:        v.IdentityV3.UserAgent,
			TokenID:          token.ID,
			EndpointLocator: func(opts gophercloud.EndpointOpts) (string, error) {
				return openstack.V3EndpointURL(catalog, opts)
			},
		},
		serializable: serializableToken{
			Token:          *token,
			TokenData:      tokenData,
			ServiceCatalog: catalog.Entries,
		},
	}
}

// TokenResult is the interface type for the argument of
// TokenValidator.TokenFromGophercloudResult().
//
// Notable implementors are tokens.CreateResult or tokens.GetResult from package
// github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens.
type TokenResult interface {
	ExtractInto(value any) error
	Extract() (*tokens.Token, error)
	ExtractServiceCatalog() (*tokens.ServiceCatalog, error)
}

var (
	// this verifies that the respective Result types from Gophercloud implement our interface
	_ TokenResult = tokens.CreateResult{}
	_ TokenResult = tokens.GetResult{}
)

type keystoneToken struct {
	DomainScope  keystoneTokenThing         `json:"domain"`
	ProjectScope keystoneTokenThingInDomain `json:"project"`
	Roles        []keystoneTokenThing       `json:"roles"`
	User         keystoneTokenThingInDomain `json:"user"`
	//NOTE: `.token.application_credential` is a non-standard extension in SAP Converged Cloud.
	ApplicationCredential keystoneTokenThing `json:"application_credential"`
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
			"user_id":                     t.User.ID,
			"user_name":                   t.User.Name,
			"user_domain_id":              t.User.Domain.ID,
			"user_domain_name":            t.User.Domain.Name,
			"domain_id":                   t.DomainScope.ID,
			"domain_name":                 t.DomainScope.Name,
			"project_id":                  t.ProjectScope.ID,
			"project_name":                t.ProjectScope.Name,
			"project_domain_id":           t.ProjectScope.Domain.ID,
			"project_domain_name":         t.ProjectScope.Domain.Name,
			"tenant_id":                   t.ProjectScope.ID,
			"tenant_name":                 t.ProjectScope.Name,
			"tenant_domain_id":            t.ProjectScope.Domain.ID,
			"tenant_domain_name":          t.ProjectScope.Domain.Name,
			"application_credential_id":   t.ApplicationCredential.ID,
			"application_credential_name": t.ApplicationCredential.Name,
			// NOTE: When adding new elements, also adjust the serialization
			// functions in `serialize.go` as necessary.
		},
		Request: map[string]string{},
	}
	for key, value := range c.Auth {
		if value == "" {
			delete(c.Auth, key)
		}
	}
	for _, role := range t.Roles {
		c.Roles = append(c.Roles, role.Name)
	}

	return c
}
