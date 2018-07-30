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
	"time"

	jwt "github.com/dgrijalva/jwt-go"
	uuid "github.com/satori/go.uuid"
)

//Request contains the query parameters and credentials in a token request.
type Request struct {
	UserName     string
	Password     string
	Scope        *Scope
	Service      string
	ClientID     string
	OfflineToken bool
}

//ParseRequest parses the data in a token request.
//
//	req, err := auth.ParseRequest(
//	    r.Header.Get("Authorization"),
//	    r.URL.RawQuery,
//	)
func ParseRequest(authorizationHeader, rawQuery string) (Request, error) {
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

	offlineToken, err := strconv.ParseBool(query.Get("offline_token"))
	if err != nil {
		offlineToken = false
	}
	result := Request{
		UserName:     username,
		Password:     password,
		Service:      service,
		ClientID:     query.Get("client_id"),
		OfflineToken: offlineToken,
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

//ToJWT creates a Java Web Token that can be used to fulfil this token request.
func (r Request) ToJWT(audience string) *jwt.Token {
	now := time.Now()
	expiry := now.Add(1 * time.Hour)

	var access []interface{}
	if r.Scope != nil {
		access = []interface{}{r.Scope}
	}

	return jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"aud": audience,
		"iss": r.Service,     //issuer (this API)
		"sub": r.UserName,    //subject
		"exp": expiry.Unix(), //not after
		"nbf": now.Unix(),    //not before
		"iat": now.Unix(),    //issued at
		//unique token ID
		"jti": uuid.NewV4().String(),
		//access permissions granted to this token
		"access": access,
	})
}
