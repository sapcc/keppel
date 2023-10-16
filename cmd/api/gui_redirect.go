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

package apicmd

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/httpext"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
)

// guiRedirecter is an api.API that implements the GUI redirect.
type guiRedirecter struct {
	db     *keppel.DB
	urlStr string
}

// AddTo implements the api.API interface.
func (g *guiRedirecter) AddTo(r *mux.Router) {
	//check if this feature is enabled
	if g.urlStr == "" {
		return
	}

	r.Methods("GET").Path("/{account:[a-z0-9-]{1,48}}/{repository:.+}").HandlerFunc(g.tryRedirectToGUI)
}

func (g *guiRedirecter) tryRedirectToGUI(w http.ResponseWriter, r *http.Request) {
	//only attempt to redirect if it's a web browser doing the request
	if !strings.Contains(r.Header.Get("Accept"), "text/html") {
		respondNotFound(w, r)
		return
	}

	vars := mux.Vars(r)

	//do we have this account/repo?
	account, err := keppel.FindAccount(g.db, vars["account"])
	if err != nil || account == nil {
		respondNotFound(w, r)
		return
	}
	repoName := stripTagAndDigest(vars["repository"])
	repo, err := keppel.FindRepository(g.db, repoName, *account)
	if err != nil || repo == nil {
		respondNotFound(w, r)
		return
	}

	//is it publicly readable?
	var policies []keppel.RBACPolicy
	_, err = g.db.WithContext(r.Context()).Select(&policies,
		"SELECT * FROM rbac_policies WHERE can_anon_pull AND account_name = $1",
		account.Name,
	)
	if err != nil {
		respondNotFound(w, r)
		return
	}
	for _, policy := range policies {
		ip := httpext.GetRequesterIPFor(r)
		if policy.Matches(ip, repo.FullName(), auth.AnonymousUserIdentity.UserName()) {
			//do the redirect
			s := g.urlStr
			s = strings.Replace(s, "%AUTH_TENANT_ID%", account.AuthTenantID, -1)
			s = strings.Replace(s, "%ACCOUNT_NAME%", account.Name, -1)
			s = strings.Replace(s, "%REPO_NAME%", repo.Name, -1)
			w.Header().Set("Location", s)
			w.WriteHeader(http.StatusFound)
			return
		}
	}

	respondNotFound(w, r)
}

func stripTagAndDigest(repoRef string) string {
	repoRef, _, _ = strings.Cut(repoRef, ":")
	repoRef, _, _ = strings.Cut(repoRef, "@")
	return repoRef
}

func respondNotFound(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "404 page not found", http.StatusNotFound)
}
