// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/sapcc/keppel/internal/keppel"
)

// AuthChallenge contains the parsed contents of a Www-Authenticate header
// returned by a registry.
type AuthChallenge struct {
	Realm   string
	Service string
	Scope   string
}

var challengeFieldRx = regexp.MustCompile(`^(\w+)\s*=\s*"([^"]*)"\s*,?\s*`)

// ParseAuthChallenge parses the auth challenge from the response headers of an
// unauthenticated request to a registry API.
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
		// find next challenge field (because of the ^ anchor, this always yields a
		// prefix of `input`)
		match := challengeFieldRx.FindStringSubmatch(input)
		if match == nil {
			return AuthChallenge{}, fmt.Errorf("malformed Www-Authenticate header: %s", hdr.Get("Www-Authenticate"))
		}

		// remove challenge field from input for next loop iteration
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

// GetToken obtains a token that satisfies this challenge.
func (c AuthChallenge) GetToken(ctx context.Context, userName, password string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Realm, http.NoBody)
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

	var data struct {
		AccessToken string `json:"access_token"`
		Errors      []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
		Token string `json:"token"`
	}
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err == nil {
		err = resp.Body.Close()
	} else {
		resp.Body.Close()
	}

	switch {
	case err != nil:
		return "", err
	case len(data.Errors) > 0:
		var errMsg string
		if data.Errors[0].Message == "" {
			errMsg = "<no message>"
		} else {
			errMsg = data.Errors[0].Message
		}
		return "", fmt.Errorf("auth token request to %q did return: %q", req.URL, errMsg)
	case data.Token != "":
		return data.Token, nil
	case data.AccessToken != "":
		return data.AccessToken, nil
	default:
		return "", errors.New("no token was returned")
	}
}
