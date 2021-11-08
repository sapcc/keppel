/******************************************************************************
*
*  Copyright 2021 SAP SE
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

package registryv2

import (
	"net/http"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/tokenauth"
)

//APIAuthorization is the interface shared by the different token mechanisms
//supported by our Registry v2 API implementation.
type APIAuthorization interface {
	//Returns the underlying keppel.UserIdentity instance. This should not be
	//used for permission checking, only for the other methods provided by the
	//UserIdentity interface.
	UserIdentity() keppel.UserIdentity
	//For use with the /v2/_catalog endpoint. Returns the names of all accounts
	//that can be listed with this authorization. If `markerAccountName` is not
	//empty, only accounts with `name > markerAccountName` will be returned.
	AccountsWithCatalogAccess(markerAccountName string) ([]string, error)
}

func (a *API) requireAuthorization(w http.ResponseWriter, r *http.Request, scope *auth.Scope) APIAuthorization {
	if r.Header.Get("Authorization") == "keppel" && !a.cfg.IsAnycastRequest(r) {
		return a.requireKeppelAPIAuth(w, r, scope, tokenauth.LocalService)
	}
	//this is also called when requireKeppelAPIAuth() returned nil, in order to render the regular 401 response
	return a.requireBearerToken(w, r, scope)
}

func isKeppelAccountViewScope(s auth.Scope) bool {
	if s.ResourceType != "keppel_account" {
		return false
	}
	for _, action := range s.Actions {
		if action == "view" {
			return true
		}
	}
	return false
}

////////////////////////////////////////////////////////////////////////////////
// type bearerToken

//bearerToken is an APIAuthorization through a supplied Bearer token (the
//standard auth method on the Registry v2 API).
type bearerToken struct {
	t *tokenauth.Token
}

func (a *API) requireBearerToken(w http.ResponseWriter, r *http.Request, scope *auth.Scope) APIAuthorization {
	//for requests to the anycast endpoint, we need to use the anycast issuer key instead of the regular one
	audience := tokenauth.LocalService
	if a.cfg.IsAnycastRequest(r) {
		audience = tokenauth.AnycastService

		//completely forbid write operations on the anycast API (only the local API
		//may be used for writes and deletes)
		if r.Method != "HEAD" && r.Method != "GET" {
			msg := "write access is not supported for anycast requests"
			keppel.ErrUnsupported.With(msg).WriteAsRegistryV2ResponseTo(w, r)
			return nil
		}
	}

	token, err := tokenauth.ParseTokenFromRequest(r, a.cfg, a.ad, audience)
	if err == nil && scope != nil && !token.Contains(*scope) {
		err = keppel.ErrDenied.With("token does not cover scope %s", scope.String())
	}
	if err != nil {
		logg.Debug("GET %s: %s", r.URL.Path, err.Error())
		challenge := tokenauth.Challenge{Service: audience, Scope: scope}
		requestURL := keppel.OriginalRequestURL(r)
		challenge.OverrideAPIHost = requestURL.Host
		challenge.OverrideAPIScheme = requestURL.Scheme
		if token != nil {
			challenge.Error = "insufficient_scope"
		}
		challenge.WriteTo(w.Header(), a.cfg)
		err.WriteAsRegistryV2ResponseTo(w, r)
		return nil
	}
	return bearerToken{token}
}

//UserIdentity implements the APIAuthorization interface.
func (b bearerToken) UserIdentity() keppel.UserIdentity {
	return b.t.UserIdentity
}

//AccountsWithCatalogAccess implements the APIAuthorization interface.
func (b bearerToken) AccountsWithCatalogAccess(markerAccountName string) ([]string, error) {
	result := make([]string, 0, len(b.t.Access))
	for _, scope := range b.t.Access {
		if !isKeppelAccountViewScope(scope) {
			continue
		}
		accountName := scope.ResourceName
		//when paginating, we don't need to care about accounts before the marker
		if markerAccountName == "" || accountName >= markerAccountName {
			result = append(result, accountName)
		}
	}
	return result, nil
}

////////////////////////////////////////////////////////////////////////////////
// type keppelAPIAuth

type keppelAPIAuth struct {
	UID           keppel.UserIdentity
	GrantedScopes auth.ScopeSet
}

func (a *API) requireKeppelAPIAuth(w http.ResponseWriter, r *http.Request, scope *auth.Scope, audience tokenauth.Service) APIAuthorization {
	uid, authErr := a.ad.AuthenticateUserFromRequest(r)
	if authErr != nil {
		//fallback to default auth to render 401 response including auth challenge
		return a.requireBearerToken(w, r, scope)
	}

	if scope == nil {
		return keppelAPIAuth{uid, nil}
	}

	grantedScopes, err := tokenauth.FilterAuthorized(auth.ScopeSet{scope}, uid, audience, a.db)
	if respondWithError(w, r, err) {
		return nil
	}
	if !grantedScopes.Contains(*scope) {
		//fallback to default auth to render 401 response including auth challenge
		return a.requireBearerToken(w, r, scope)
	}

	return keppelAPIAuth{uid, grantedScopes}
}

//UserIdentity implements the APIAuthorization interface.
func (a keppelAPIAuth) UserIdentity() keppel.UserIdentity {
	return a.UID
}

//AccountsWithCatalogAccess implements the APIAuthorization interface.
func (a keppelAPIAuth) AccountsWithCatalogAccess(markerAccountName string) ([]string, error) {
	result := make([]string, 0, len(a.GrantedScopes))
	for _, scope := range a.GrantedScopes {
		if !isKeppelAccountViewScope(*scope) {
			continue
		}
		accountName := scope.ResourceName
		//when paginating, we don't need to care about accounts before the marker
		if markerAccountName == "" || accountName >= markerAccountName {
			result = append(result, accountName)
		}
	}
	return result, nil
}
