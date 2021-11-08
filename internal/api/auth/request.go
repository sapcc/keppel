/*******************************************************************************
*
* Copyright 2018 SAP SE
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

package authapi

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/tokenauth"
)

var (
	errMalformedAuthHeader = errors.New("malformed Authorization header")
)

func (a *API) checkAuthentication(authorizationHeader string) (keppel.Authorization, error) {
	if authorizationHeader == "" {
		return keppel.AnonymousAuthorization, nil
	}
	if !strings.HasPrefix(authorizationHeader, "Basic ") {
		return nil, errMalformedAuthHeader
	}

	userName, password := decodeAuthHeader(
		strings.TrimPrefix(authorizationHeader, "Basic "))
	if userName == "" {
		return nil, errMalformedAuthHeader
	}

	if strings.HasPrefix(userName, "replication@") {
		peerHostName := strings.TrimPrefix(userName, "replication@")
		peer, err := tokenauth.CheckPeerCredentials(a.db, peerHostName, password)
		if err != nil {
			return nil, err
		}
		if peer == nil {
			return nil, sql.ErrNoRows
		}
		return keppel.ReplicationAuthorization{PeerHostName: peerHostName}, nil
	}

	authz, rerr := a.authDriver.AuthenticateUser(userName, password)
	if rerr != nil {
		return nil, rerr
	}
	return authz, nil

	//WARNING: It's tempting to shorten the last paragraph to just
	//
	//	return a.authDriver.AuthenticateUser(userName, password)
	//
	//But that breaks everything! AuthenticateUser does not return `error`, it
	//returns `*keppel.RegistryV2Error`. When a nil RegistryV2Error is returned,
	//it would get cast into
	//
	//	err = error(*keppel.RegistryV2Error(nil))
	//
	//which is very different from
	//
	//	err = error(nil)
	//
	//That's one of the few really really stupid traps in Go.
}

//Request contains the query parameters in a token request.
type Request struct {
	Scopes           tokenauth.ScopeSet
	ClientID         string
	OfflineToken     bool
	IntendedAudience tokenauth.Service
	//the auth handler may add additional scopes in addition to the originally
	//requested scope to encode access permissions, RBACs, etc.
	CompiledScopes tokenauth.ScopeSet
}

func parseRequest(rawQuery string, cfg keppel.Configuration) (Request, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return Request{}, fmt.Errorf("cannot parse query string: %s", err.Error())
	}

	offlineToken := keppel.ParseBool(query.Get("offline_token"))
	result := Request{
		ClientID:     query.Get("client_id"),
		Scopes:       parseScopes(query["scope"]),
		OfflineToken: offlineToken,
	}

	serviceHost := query.Get("service")
	if serviceHost == tokenauth.LocalService.Hostname(cfg) {
		result.IntendedAudience = tokenauth.LocalService
	} else if cfg.AnycastAPIPublicURL != nil && serviceHost == tokenauth.AnycastService.Hostname(cfg) {
		result.IntendedAudience = tokenauth.AnycastService
	} else {
		return Request{}, fmt.Errorf("cannot issue tokens for service: %q", serviceHost)
	}

	return result, nil
}

func decodeAuthHeader(base64data string) (username, password string) {
	bytes, err := base64.StdEncoding.DecodeString(base64data)
	if err != nil {
		return "", ""
	}

	fields := strings.SplitN(string(bytes), ":", 2)
	if len(fields) != 2 {
		return "", ""
	}
	return fields[0], fields[1]
}

//ToToken creates a token that can be used to fulfill this token request.
func (r Request) ToToken(authz keppel.Authorization) tokenauth.Token {
	var access []tokenauth.Scope
	for _, scope := range append(r.Scopes, r.CompiledScopes...) {
		if len(scope.Actions) > 0 {
			access = append(access, *scope)
		}
	}

	return tokenauth.Token{
		Authorization: authz,
		Audience:      r.IntendedAudience,
		Access:        access,
	}
}
