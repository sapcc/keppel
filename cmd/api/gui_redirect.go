// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package apicmd

import (
	"net/http"
	"slices"
	"strings"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/httpext"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// guiRedirecter is an api.API that implements the GUI redirect.
type guiRedirecter struct {
	db     *keppel.DB
	urlStr string
}

// AddTo implements the api.API interface.
func (g *guiRedirecter) AddTo(r *mux.Router) {
	// check if this feature is enabled
	if g.urlStr == "" {
		return
	}

	r.Methods("GET").Path("/{account:[a-z0-9][a-z0-9-]{0,47}}/{repository:.+}").HandlerFunc(g.tryRedirectToGUI)
}

func (g *guiRedirecter) tryRedirectToGUI(w http.ResponseWriter, r *http.Request) {
	// only attempt to redirect if it's a web browser doing the request
	if !strings.Contains(r.Header.Get("Accept"), "text/html") {
		respondNotFound(w, r)
		return
	}

	vars := mux.Vars(r)

	// do we have this account/repo?
	accountName := models.AccountName(vars["account"])
	account, err := keppel.FindAccount(g.db, accountName)
	if err != nil || account == nil {
		respondNotFound(w, r)
		return
	}
	repoName := stripTagAndDigest(vars["repository"])
	repo, err := keppel.FindRepository(g.db, repoName, accountName)
	if err != nil || repo == nil {
		respondNotFound(w, r)
		return
	}

	// is it publicly readable?
	policies, err := keppel.ParseRBACPolicies(*account)
	if err != nil {
		respondNotFound(w, r)
		return
	}
	for _, policy := range policies {
		if !slices.Contains(policy.Permissions, keppel.RBACAnonymousPullPermission) {
			continue
		}
		ip := httpext.GetRequesterIPFor(r)
		if policy.Matches(ip, repo.Name, auth.AnonymousUserIdentity.UserName()) {
			// do the redirect
			s := g.urlStr
			s = strings.ReplaceAll(s, "%AUTH_TENANT_ID%", account.AuthTenantID)
			s = strings.ReplaceAll(s, "%ACCOUNT_NAME%", string(account.Name))
			s = strings.ReplaceAll(s, "%REPO_NAME%", repo.Name)
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
