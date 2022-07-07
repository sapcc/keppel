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
	"fmt"
	"net/url"
	"strconv"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
)

//Request contains the query parameters in a token request.
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
