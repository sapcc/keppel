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

	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/go-bits/sre"
	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
)

var requiredScopeForCatalogEndpoint = auth.Scope{
	ResourceType: "registry",
	ResourceName: "catalog",
	Actions:      []string{"*"},
}

const maxLimit = 100

//This implements the GET /v2/_catalog endpoint.
func (a *API) handleGetCatalog(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/v2/_catalog")
	//must be set even for 401 responses!
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	//defense in depth: the auth API does not issue anycast tokens for registry:catalog:* anyway
	if a.cfg.IsAnycastRequest(r) {
		msg := "/v2/_catalog endpoint is not supported for anycast requests"
		keppel.ErrUnsupported.With(msg).WriteAsRegistryV2ResponseTo(w, r)
		return
	}

	token := a.requireBearerToken(w, r, &requiredScopeForCatalogEndpoint)
	if token == nil {
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

	//parse query: marker (parameter "last")
	marker := query.Get("last")
	markerAccountName := ""
	if marker != "" {
		fields := strings.SplitN(marker, "/", 2)
		if len(fields) != 2 {
			http.Error(w, `invalid value for "last": must contain a slash`, http.StatusBadRequest)
			return
		}
		markerAccountName = fields[0]
	}

	//find accessible accounts
	var accounts []*keppel.Account
	for _, scope := range token.Access {
		accountName := parseKeppelAccountScope(scope)
		if accountName == "" {
			//`scope` does not look like `keppel_account:$ACCOUNT_NAME:view`
			continue
		}
		account, err := keppel.FindAccount(a.db, accountName)
		if respondWithError(w, r, err) {
			return
		}
		if account == nil {
			//account was deleted since token issuance, so there cannot be any repos
			//in it
			continue
		}
		//when paginating, we don't need to care about accounts before the marker
		if markerAccountName == "" || account.Name >= markerAccountName {
			accounts = append(accounts, account)
		}
	}
	sort.Slice(accounts, func(i, j int) bool {
		return accounts[i].Name < accounts[j].Name
	})

	//collect repository names from backend
	var allNames []string
	partialResult := false
	for idx, account := range accounts {
		names, err := a.getCatalogForAccount(*account)
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

func parseKeppelAccountScope(s auth.Scope) string {
	if s.ResourceType != "keppel_account" {
		return ""
	}
	for _, action := range s.Actions {
		if action == "view" {
			return s.ResourceName
		}
	}
	return ""
}

const catalogGetQuery = `SELECT name FROM repos WHERE account_name = $1 ORDER BY name`

func (a *API) getCatalogForAccount(account keppel.Account) ([]string, error) {
	var result []string
	err := keppel.ForeachRow(a.db, catalogGetQuery, []interface{}{account.Name},
		func(rows *sql.Rows) error {
			var name string
			err := rows.Scan(&name)
			if err == nil {
				result = append(result, fmt.Sprintf("%s/%s", account.Name, name))
			}
			return err
		},
	)
	return result, err
}
