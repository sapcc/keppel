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
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
)

var requiredScopeForCatalogEndpoint = auth.Scope{
	ResourceType: "registry",
	ResourceName: "catalog",
	Actions:      []string{"*"},
}

//This implements the GET /v2/_catalog endpoint.
func (a *API) handleProxyCatalog(w http.ResponseWriter, r *http.Request) {
	//must be set even for 401 responses!
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

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
		limit = math.MaxUint64
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
		account, err := a.db.FindAccount(accountName)
		if respondWithError(w, err) {
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
		names, ok := a.getCatalogForAccount(w, *account, r.Header.Get("Authorization"))
		if !ok {
			//in this case, getCatalogForAccount has rendered an error onto `w` already
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

func (a *API) getCatalogForAccount(w http.ResponseWriter, account keppel.Account, authorizationHeader string) (names []string, ok bool) {
	//NOTE: This reuses the user's token, which works because everyone
	//(keppel-api and each keppel-registry) expects the same token audience.
	//However, this is unsound: If the user has access to keppel-registry
	//directly (which is a feature we might offer in the future), they could use
	//a registry:catalog:* token intended for keppel-api to list repos in
	//keppel-registry, even if they don't have the corresponding
	//keppel_account:$NAME:view scope. If direct access to keppel-registry is
	//desired, the auth API should be changed to issue tokens for different
	//audiences.

	//build request
	req, err := http.NewRequest("GET", "/v2/_catalog", nil)
	if respondWithError(w, err) {
		return nil, false
	}
	req.Header.Set("Authorization", authorizationHeader)

	//perform request to backing keppel-registry
	resp, err := a.orchestrationDriver.DoHTTPRequest(account, req, keppel.FollowRedirects)
	if respondWithError(w, err) {
		return nil, false
	}
	respBodyBytes, err := ioutil.ReadAll(resp.Body)
	if respondWithError(w, err) {
		return nil, false
	}
	err = resp.Body.Close()
	if respondWithError(w, err) {
		return nil, false
	}

	//in case of unexpected errors, forward error message to user
	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.Write(respBodyBytes)
	}

	//decode response body
	var data struct {
		Repositories []string `json:"repositories"`
	}
	dec := json.NewDecoder(bytes.NewReader(respBodyBytes))
	dec.DisallowUnknownFields()
	err = dec.Decode(&data)
	if respondWithError(w, err) {
		return nil, false
	}

	return data.Repositories, true
}
