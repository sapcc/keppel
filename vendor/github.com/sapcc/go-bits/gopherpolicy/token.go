// SPDX-FileCopyrightText: 2017-2018 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package gopherpolicy

import (
	"fmt"
	"net/http"

	policy "github.com/databus23/goslo.policy"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/sapcc/go-api-declarations/cadf"

	"github.com/sapcc/go-bits/internal"
)

// Enforcer contains the Enforce method that struct Token requires to check
// access permissions. This interface is satisfied by struct Enforcer from
// goslo.policy.
type Enforcer interface {
	Enforce(rule string, c policy.Context) bool
}

// Token represents a validated Keystone v3 token. It is returned from
// Validator.CheckToken().
type Token struct {
	// The enforcer that checks access permissions for this client token. Usually
	// an instance of struct Enforcer from goslo.policy. Usually inherited from
	// struct TokenValidator.
	Enforcer Enforcer
	// When AuthN succeeds, contains information about the client token which can
	// be used to check access permissions.
	Context policy.Context
	// When AuthN succeeds, contains a fully-initialized ProviderClient with which
	// this process can use the OpenStack API on behalf of the authenticated user.
	ProviderClient *gophercloud.ProviderClient
	// When AuthN fails, contains the deferred AuthN error.
	Err error

	// When AuthN succeeds, contains all the information needed to serialize this
	// token in SerializeTokenForCache.
	serializable serializableToken

	// WARNING: Do not add new unexported fields here, unless you have a specific plan
	// how they can survive a serialization roundtrip (both within this package,
	// through type serializableToken; or outside this package, by reconstructing
	// a token from the result of DeserializeCompactContextFromJSON()).
}

// Require checks if the given token has the given permission according to the
// policy.json that is in effect. If not, an error response is written and false
// is returned.
func (t *Token) Require(w http.ResponseWriter, rule string) bool {
	if t.Err != nil {
		if t.Context.Logger != nil {
			t.Context.Logger(fmt.Sprintf("returning %v because of error: %s", http.StatusUnauthorized, t.Err.Error()))
		}
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}

	if !t.Enforcer.Enforce(rule, t.Context) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return false
	}
	return true
}

// Check is like Require, but does not write error responses.
func (t *Token) Check(rule string) bool {
	return t.Err == nil && t.Enforcer.Enforce(rule, t.Context)
}

// UserUUID returns the UUID of the user for whom this token was issued, or ""
// if the token was invalid.
func (t *Token) UserUUID() string {
	return t.Context.Auth["user_id"]
}

// UserName returns the name of the user for whom this token was issued, or ""
// if the token was invalid.
func (t *Token) UserName() string {
	return t.Context.Auth["user_name"]
}

// UserDomainName returns the name of the domain containing the user for whom
// this token was issued, or "" if the token was invalid.
func (t *Token) UserDomainName() string {
	return t.Context.Auth["user_domain_name"]
}

// UserDomainUUID returns the UUID of the domain containing the user for whom
// this token was issued, or "" if the token was invalid.
func (t *Token) UserDomainUUID() string {
	return t.Context.Auth["user_domain_id"]
}

// ProjectScopeUUID returns the UUID of this token's project scope, or "" if the token is
// invalid or not scoped to a project.
func (t *Token) ProjectScopeUUID() string {
	return t.Context.Auth["project_id"]
}

// ProjectScopeName returns the name of this token's project scope, or "" if the token is
// invalid or not scoped to a project.
func (t *Token) ProjectScopeName() string {
	return t.Context.Auth["project_name"]
}

// ProjectScopeDomainUUID returns the UUID of this token's project scope domain, or ""
// if the token is invalid or not scoped to a project.
func (t *Token) ProjectScopeDomainUUID() string {
	return t.Context.Auth["project_domain_id"]
}

// ProjectScopeDomainName returns the name of this token's project scope domain, or ""
// if the token is invalid or not scoped to a project.
func (t *Token) ProjectScopeDomainName() string {
	return t.Context.Auth["project_domain_name"]
}

// DomainScopeUUID returns the UUID of this token's domain scope, or "" if the token is
// invalid or not scoped to a domain.
func (t *Token) DomainScopeUUID() string {
	return t.Context.Auth["domain_id"]
}

// DomainScopeName returns the name of this token's domain scope, or "" if the token is
// invalid or not scoped to a domain.
func (t *Token) DomainScopeName() string {
	return t.Context.Auth["domain_name"]
}

// ApplicationCredentialID returns the ID of the application credential that
// was used to create this token, or "" if the token was created through a
// different authentication method.
func (t *Token) ApplicationCredentialID() string {
	return t.Context.Auth["application_credential_id"]
}

// IsAdminProject returns whether the token is scoped to the project that is
// designated for cloud administrators within Keystone (if any).
func (t *Token) IsAdminProject() bool {
	return t.Context.Auth["is_admin_project"] == formatBoolLikePython(true)
}

// AsInitiator implements the audittools.UserInfo interface.
func (t *Token) AsInitiator(host cadf.Host) cadf.Resource {
	return cadf.Resource{
		TypeURI: internal.StandardUserInfoTypeURI,
		// information about user
		Name:   t.UserName(),
		Domain: t.UserDomainName(),
		ID:     t.UserUUID(),
		Host:   &host,
		// information about user's scope (only one of both will be filled)
		DomainID:          t.DomainScopeUUID(),
		DomainName:        t.DomainScopeName(),
		ProjectID:         t.ProjectScopeUUID(),
		ProjectName:       t.ProjectScopeName(),
		ProjectDomainName: t.ProjectScopeDomainName(),
		AppCredentialID:   t.ApplicationCredentialID(),
	}
}

////////////////////////////////////////////////////////////////////////////////
// type serializableToken

type serializableToken struct {
	Token          tokens.Token          `json:"token_id"`
	TokenData      keystoneToken         `json:"token_data"`
	ServiceCatalog []tokens.CatalogEntry `json:"catalog"`
}

// ExtractInto implements the TokenResult interface.
func (s serializableToken) ExtractInto(value any) error {
	// TokenResult.ExtractInto is only ever called with a value of type
	// *keystoneToken, so this is okay
	kd, ok := value.(*keystoneToken)
	if !ok {
		return fmt.Errorf("serializableToken.ExtractInto called with unsupported target type %T", value)
	}
	*kd = s.TokenData
	return nil
}

// Extract implements the TokenResult interface.
func (s serializableToken) Extract() (*tokens.Token, error) {
	return &s.Token, nil
}

// ExtractServiceCatalog implements the TokenResult interface.
func (s serializableToken) ExtractServiceCatalog() (*tokens.ServiceCatalog, error) {
	return &tokens.ServiceCatalog{Entries: s.ServiceCatalog}, nil
}
