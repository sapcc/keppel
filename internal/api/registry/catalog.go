/******************************************************************************
*
*  Copyright 2019 SAP SE
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
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/auth"
)

const maxLimit = 100

// This implements the GET /v2/_catalog endpoint.
func (a *API) handleGetCatalog(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/v2/_catalog")
	//must be set even for 401 responses!
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	authz, rerr := auth.IncomingRequest{
		HTTPRequest:           r,
		Scopes:                auth.NewScopeSet(auth.CatalogEndpointScope),
		AllowsAnycast:         false,
		AllowsDomainRemapping: true,
	}.Authorize(a.cfg, a.ad, a.db)
	if rerr != nil {
		rerr.WriteAsRegistryV2ResponseTo(w, r)
		return
	}

	//parse query: limit (parameter "n")
	query := r.URL.Query()
	var (
		limit uint64
		err   error
	)
	if limitStr := query.Get("n"); limitStr != "" {
		limit, err = strconv.ParseUint(limitStr, 10, 64)
		if err != nil {
			http.Error(w, `invalid value for "n": `+err.Error(), http.StatusBadRequest)
			return
		}
		if limit == 0 {
			http.Error(w, `invalid value for "n": must not be 0`, http.StatusBadRequest)
			return
		}
	} else {
		limit = maxLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	//on domain-remapped APIs, do not include the account name in the repository
	//names for the result list
	includeAccountName := authz.Audience.AccountName == ""

	//parse query: marker (parameter "last")
	marker := query.Get("last")
	markerAccountName := ""
	if marker != "" {
		if includeAccountName {
			fields := strings.SplitN(marker, "/", 2)
			if len(fields) != 2 {
				http.Error(w, `invalid value for "last": must contain a slash`, http.StatusBadRequest)
				return
			}
			markerAccountName = fields[0]
		} else {
			markerAccountName = authz.Audience.AccountName
		}
	}

	//find accessible accounts
	accountNames := authz.ScopeSet.AccountsWithCatalogAccess(markerAccountName)
	sort.Strings(accountNames)

	//collect repository names from backend
	var allNames []string
	partialResult := false
	for idx, accountName := range accountNames {
		names, err := a.getCatalogForAccount(accountName, includeAccountName)
		if respondWithError(w, r, err) {
			return
		}

		//when paginating, we might start in the middle of the first account's repo list
		if idx == 0 && marker != "" {
			filteredNames := make([]string, 0, len(names))
			for _, name := range names {
				if marker < name {
					filteredNames = append(filteredNames, name)
				}
			}
			names = filteredNames
		}
		sort.Strings(names)
		allNames = append(allNames, names...)

		//stop asking further accounts for repos once we overflow the current page
		if uint64(len(allNames)) > limit {
			allNames = allNames[0:limit]
			partialResult = true
		}
	}

	//write response
	if partialResult {
		linkQuery := url.Values{}
		linkQuery.Set("n", strconv.FormatUint(limit, 10))
		linkQuery.Set("last", allNames[len(allNames)-1])
		linkURL := url.URL{Path: "/v2/_catalog", RawQuery: linkQuery.Encode()}
		w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, linkURL.String()))
	}
	if len(allNames) == 0 {
		allNames = []string{}
	}
	respondwith.JSON(w, http.StatusOK, map[string]interface{}{
		"repositories": allNames,
	})
}

const catalogGetQuery = `SELECT name FROM repos WHERE account_name = $1 ORDER BY name`

func (a *API) getCatalogForAccount(accountName string, includeAccountName bool) ([]string, error) {
	var result []string
	err := sqlext.ForeachRow(a.db, catalogGetQuery, []interface{}{accountName},
		func(rows *sql.Rows) error {
			var name string
			err := rows.Scan(&name)
			if err == nil {
				if includeAccountName {
					result = append(result, fmt.Sprintf("%s/%s", accountName, name))
				} else {
					result = append(result, name)
				}
			}
			return err
		},
	)
	return result, err
}
