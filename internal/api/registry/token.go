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
)

//APIAuthorization is the interface shared by the different token mechanisms
//supported by our Registry v2 API implementation.
type APIAuthorization interface {
	//Returns the underlying keppel.Authorization instance. This should not be
	//used for permission checking, only for the other methods provided by the
	//Authorization interface.
	Authorization() keppel.Authorization
	//For use with the /v2/_catalog endpoint. Returns the names of all accounts
	//that can be listed with this authorization. If `markerAccountName` is not
	//empty, only accounts with `name > markerAccountName` will be returned.
	AccountsWithCatalogAccess(markerAccountName string) ([]string, error)
}

func (a *API) requireAuthorization(w http.ResponseWriter, r *http.Request, scope *auth.Scope) APIAuthorization {
	//TODO keppelToken
	return a.requireBearerToken(w, r, scope)
}

////////////////////////////////////////////////////////////////////////////////
// type bearerToken

//bearerToken is an APIAuthorization through a supplied Bearer token (the
//standard auth method on the Registry v2 API).
type bearerToken struct {
	t *auth.Token
}

func (a *API) requireBearerToken(w http.ResponseWriter, r *http.Request, scope *auth.Scope) APIAuthorization {
	//for requests to the anycast endpoint, we need to use the anycast issuer key instead of the regular one
	audience := auth.LocalService
	if a.cfg.IsAnycastRequest(r) {
		audience = auth.AnycastService

		//completely forbid write operations on the anycast API (only the local API
		//may be used for writes and deletes)
		if r.Method != "HEAD" && r.Method != "GET" {
			msg := "write access is not supported for anycast requests"
			keppel.ErrUnsupported.With(msg).WriteAsRegistryV2ResponseTo(w, r)
			return nil
		}
	}

	token, err := auth.ParseTokenFromRequest(r, a.cfg, a.ad, audience)
	if err == nil && scope != nil && !token.Contains(*scope) {
		err = keppel.ErrDenied.With("token does not cover scope %s", scope.String())
	}
	if err != nil {
		logg.Debug("GET %s: %s", r.URL.Path, err.Error())
		challenge := auth.Challenge{Service: audience, Scope: scope}
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

//Authorization implements the APIAuthorization interface.
func (b bearerToken) Authorization() keppel.Authorization {
	return b.t.Authorization
}

//AccountsWithCatalogAccess implements the APIAuthorization interface.
func (b bearerToken) AccountsWithCatalogAccess(markerAccountName string) ([]string, error) {
	result := make([]string, 0, len(b.t.Access))
	for _, scope := range b.t.Access {
		accountName := parseKeppelAccountScope(scope)
		if accountName == "" {
			//`scope` does not look like `keppel_account:$ACCOUNT_NAME:view`
			continue
		}
		//when paginating, we don't need to care about accounts before the marker
		if markerAccountName == "" || accountName >= markerAccountName {
			result = append(result, accountName)
		}
	}
	return result, nil
}
