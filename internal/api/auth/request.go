// SPDX-FileCopyrightText: 2018 SAP SE
// SPDX-License-Identifier: Apache-2.0

package authapi

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
)

// Request contains the query parameters in a token request.
type Request struct {
	Scopes           auth.ScopeSet
	ClientID         string
	OfflineToken     bool
	IntendedAudience auth.Audience
}

func parseRequest(rawQuery string, cfg keppel.Configuration) (Request, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return Request{}, fmt.Errorf("cannot parse query string: %s", err.Error())
	}

	offlineToken, err := strconv.ParseBool(query.Get("offline_token"))
	result := Request{
		ClientID:     query.Get("client_id"),
		Scopes:       parseScopes(query["scope"]),
		OfflineToken: offlineToken && err == nil,
	}

	serviceHost := query.Get("service")
	result.IntendedAudience = auth.IdentifyAudience(serviceHost, cfg)
	if result.IntendedAudience.Hostname(cfg) != serviceHost {
		return Request{}, fmt.Errorf("cannot issue tokens for service: %q", serviceHost)
	}

	return result, nil
}
