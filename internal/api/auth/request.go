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
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
)

var (
	errNoAuthHeader        = errors.New("missing Authorization header")
	errMalformedAuthHeader = errors.New("malformed Authorization header")
)

func (a *API) checkAuthentication(authorizationHeader string) (userName string, authz keppel.Authorization, err error) {
	if authorizationHeader == "" {
		//TODO support anonymous login (only required once ACLs are added)
		return "", nil, errNoAuthHeader
	}
	if !strings.HasPrefix(authorizationHeader, "Basic ") {
		return "", nil, errMalformedAuthHeader
	}

	userName, password := decodeAuthHeader(
		strings.TrimPrefix(authorizationHeader, "Basic "))
	if userName == "" {
		return "", nil, errMalformedAuthHeader
	}

	authz, rerr := a.authDriver.AuthenticateUser(userName, password)
	if rerr != nil {
		return "", nil, rerr
	}

	return userName, authz, nil
}

//Request contains the query parameters in a token request.
type Request struct {
	Scope            auth.Scope
	ClientID         string
	OfflineToken     bool
	IntendedAudience string
	//the auth handler may add additional scopes in addition to the originally
	//requested scope to encode access permissions, RBACs, etc.
	CompiledScopes []auth.Scope
}

func parseRequest(rawQuery string, cfg keppel.Configuration) (Request, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return Request{}, fmt.Errorf("cannot parse query string: %s", err.Error())
	}

	offlineToken, err := strconv.ParseBool(query.Get("offline_token"))
	if err != nil {
		offlineToken = false
	}
	result := Request{
		ClientID:         query.Get("client_id"),
		Scope:            parseScope(query.Get("scope")),
		OfflineToken:     offlineToken,
		IntendedAudience: query.Get("service"),
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

//ToToken creates a token that can be used to fulfil this token request.
func (r Request) ToToken(userName string) auth.Token {
	var access []auth.Scope
	if len(r.Scope.Actions) > 0 {
		access = []auth.Scope{r.Scope}
	}
	for _, scope := range r.CompiledScopes {
		if len(scope.Actions) > 0 {
			access = append(access, scope)
		}
	}

	return auth.Token{
		UserName: userName,
		Audience: r.IntendedAudience,
		Access:   access,
	}
}
