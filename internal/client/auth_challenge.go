/******************************************************************************
*
*  Copyright 2020 SAP SE
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

package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/sapcc/keppel/internal/keppel"
)

//AuthChallenge contains the parsed contents of a Www-Authenticate header
//returned by a registry.
type AuthChallenge struct {
	Realm   string
	Service string
	Scope   string
}

var challengeFieldRx = regexp.MustCompile(`^(\w+)\s*=\s*"([^"]*)"\s*,?\s*`)

//ParseAuthChallenge parses the auth challenge from the response headers of an
//unauthenticated request to a registry API.
func ParseAuthChallenge(hdr http.Header) (AuthChallenge, error) {
	input := hdr.Get("Www-Authenticate")
	if input == "" {
		return AuthChallenge{}, errors.New("missing Www-Authenticate header")
	}
	if !strings.HasPrefix(input, "Bearer ") {
		parts := strings.SplitN(input, " ", 2)
		return AuthChallenge{}, fmt.Errorf("cannot handle Www-Authenticate challenge of type %q", parts[0])
	}
	input = strings.TrimSpace(strings.TrimPrefix(input, "Bearer "))

	var c AuthChallenge

	for input != "" {
		//find next challenge field (because of the ^ anchor, this always yields a
		//prefix of `input`)
		match := challengeFieldRx.FindStringSubmatch(input)
		if match == nil {
			return AuthChallenge{}, fmt.Errorf("malformed Www-Authenticate header: %s", hdr.Get("Www-Authenticate"))
		}

		//remove challenge field from input for next loop iteration
		input = strings.TrimPrefix(input, match[0])

		key, value := match[1], match[2]
		switch key {
		case "realm":
			c.Realm = value
		case "service":
			c.Service = value
		case "scope":
			c.Scope = value
		}
	}

	if c.Realm == "" {
		return AuthChallenge{}, fmt.Errorf("missing realm in Www-Authenticate: Bearer %s", input)
	}
	if c.Service == "" {
		return AuthChallenge{}, fmt.Errorf("missing service in Www-Authenticate: Bearer %s", input)
	}
	if c.Scope == "" {
		return AuthChallenge{}, fmt.Errorf("missing scope in Www-Authenticate: Bearer %s", input)
	}
	return c, nil
}

//GetToken obtains a token that satisfies this challenge.
func (c AuthChallenge) GetToken(userName, password string) (string, error) {
	req, err := http.NewRequest("GET", c.Realm, nil)
	if err != nil {
		return "", err
	}
	if userName != "" {
		req.Header.Set("Authorization", keppel.BuildBasicAuthHeader(userName, password))
	}
	q := make(url.Values)
	q.Set("service", c.Service)
	q.Set("scope", c.Scope)
	req.URL.RawQuery = q.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	respBytes, err := ioutil.ReadAll(resp.Body)
	if err == nil {
		err = resp.Body.Close()
	} else {
		resp.Body.Close()
	}
	if err != nil {
		return "", err
	}

	var data struct {
		Token string `json:"token"`
	}
	err = json.Unmarshal(respBytes, &data)
	return data.Token, err
}
