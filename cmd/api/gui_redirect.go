// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package apicmd

import (
	"net/http"
	"slices"
	"strings"

	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/httpext"
	"go.xyrillian.de/gg/gsql"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// guiRedirecter is an [httpapi.API] that implements the GUI redirect.
type guiRedirecter struct {
	db     *gsql.DB
	urlStr string
}

// AddTo implements the [httpapi.API] interface.
func (g *guiRedirecter) AddTo(c *httpapi.Composer) {
	// check if this feature is enabled
	if g.urlStr == "" {
		return
	}

	c.AddTryHandler(g)
}

// TryServeHTTP implements the [httpapi.TryHandler] interface.
func (g *guiRedirecter) TryServeHTTP(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}

	// only attempt to redirect if it's a web browser doing the request
	if !strings.Contains(r.Header.Get("Accept"), "text/html") {
		return false
	}

	// the request URL will have to look like an image reference, e.g. `keppel.example.com/foo/bar:latest`, thus:
	//   -> r.URL.Path = "/foo/bar:latest"
	//   -> accountName = "foo", restOfPath = "bar:latest"
	accountNameStr, restOfPath, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if !ok {
		return false
	}
	if !models.IsAccountName(accountNameStr) {
		return false
	}
	accountName := models.AccountName(accountNameStr)

	// do we have this account/repo?
	ctx := r.Context()
	account, err := keppel.FindAccount(ctx, g.db, accountName)
	if err != nil {
		return false
	}
	repoName := stripTagAndDigest(restOfPath)
	repo, err := keppel.FindRepository(ctx, g.db, repoName, accountName)
	if err != nil {
		return false
	}

	// is it publicly readable?
	policies, err := keppel.ParseRBACPolicies(account)
	if err != nil {
		return false
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
			return true
		}
	}

	return false
}

func stripTagAndDigest(repoRef string) string {
	repoRef, _, _ = strings.Cut(repoRef, ":")
	repoRef, _, _ = strings.Cut(repoRef, "@")
	return repoRef
}
