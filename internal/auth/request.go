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

package auth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/sapcc/keppel/internal/keppel"
)

//Request contains the query parameters and credentials in a token request.
type Request struct {
	UserName     string
	Password     string
	Scope        *Scope
	ClientID     string
	OfflineToken bool
	//the auth handler may add additional scopes in addition to the originally
	//requested scope to encode access permissions, RBACs, etc.
	CompiledScopes []Scope
	//the Configuration is used later on to construct the Token
	config keppel.Configuration
}

//ParseRequest parses the data in a token request.
//
//	req, err := auth.ParseRequest(
//	    r.Header.Get("Authorization"),
//	    r.URL.RawQuery,
//	)
func ParseRequest(authorizationHeader, rawQuery string, cfg keppel.Configuration) (Request, error) {
	if !strings.HasPrefix(authorizationHeader, "Basic ") { //e.g. because it's missing
		return Request{}, errors.New("missing Authorization header")
	}
	username, password, err := decodeAuthHeader(
		strings.TrimPrefix(authorizationHeader, "Basic "))
	if err != nil {
		return Request{}, fmt.Errorf("cannot parse Authorization header: %s", err.Error())
	}

	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return Request{}, fmt.Errorf("cannot parse query string: %s", err.Error())
	}

	service := query.Get("service")
	if service == "" {
		return Request{}, errors.New("missing query parameter: service")
	}
	if service != cfg.APIPublicHostname() {
		return Request{}, errors.New("malformed query paramter: service")
	}

	offlineToken, err := strconv.ParseBool(query.Get("offline_token"))
	if err != nil {
		offlineToken = false
	}
	result := Request{
		UserName:     username,
		Password:     password,
		ClientID:     query.Get("client_id"),
		OfflineToken: offlineToken,
		config:       cfg,
	}

	scopeStr := query.Get("scope")
	if scopeStr == "" {
		if !offlineToken {
			return Request{}, errors.New("missing query parameter: scope")
		}
	} else {
		scope, err := ParseScope(scopeStr)
		if err != nil {
			return Request{}, err
		}
		result.Scope = &scope
	}

	return result, nil
}

func decodeAuthHeader(base64data string) (username, password string, err error) {
	bytes, err := base64.StdEncoding.DecodeString(base64data)
	if err != nil {
		return "", "", err
	}

	fields := strings.SplitN(string(bytes), ":", 2)
	if len(fields) != 2 {
		return "", "", errors.New(`expected "username:password" payload, but got no colon`)
	}
	return fields[0], fields[1], nil
}

//ToToken creates a token that can be used to fulfil this token request.
func (r Request) ToToken() *Token {
	var access []Scope
	if r.Scope != nil && len(r.Scope.Actions) > 0 {
		access = []Scope{*r.Scope}
	}
	for _, scope := range r.CompiledScopes {
		if len(scope.Actions) > 0 {
			access = append(access, scope)
		}
	}

	return &Token{
		UserName: r.UserName,
		Access:   access,
		config:   r.config,
	}
}
