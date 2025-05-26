// SPDX-FileCopyrightText: 2018-2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppelv1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/errext"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/respondwith"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

const SubleaseHeader = "X-Keppel-Sublease-Token"

func (a *API) handleGetAccounts(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/accounts")
	var accounts []models.Account
	_, err := a.db.Select(&accounts, "SELECT * FROM accounts ORDER BY name")
	if respondwith.ErrorText(w, err) {
		return
	}
	scopes := accountScopes(keppel.CanViewAccount, accounts...)

	authz := a.authenticateRequest(w, r, scopes)
	if authz == nil {
		return
	}
	if authz.UserIdentity.UserType() == keppel.AnonymousUser {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// restrict accounts to those visible in the current scope
	var accountsFiltered []models.Account
	for idx, account := range accounts {
		if authz.ScopeSet.Contains(*scopes[idx]) {
			accountsFiltered = append(accountsFiltered, account)
		}
	}
	// ensure that this serializes as a list, not as null
	if len(accountsFiltered) == 0 {
		accountsFiltered = []models.Account{}
	}

	// render accounts to JSON
	accountsRendered := make([]keppel.Account, len(accountsFiltered))
	for idx, account := range accountsFiltered {
		accountsRendered[idx], err = keppel.RenderAccount(account)
		if respondwith.ErrorText(w, err) {
			return
		}
	}
	respondwith.JSON(w, http.StatusOK, map[string]any{"accounts": accountsRendered})
}

func (a *API) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/accounts/:account")
	authz := a.authenticateRequest(w, r, accountScopeFromRequest(r, keppel.CanViewAccount))
	if authz == nil {
		return
	}
	account := a.findAccountFromRequest(w, r, authz)
	if account == nil {
		return
	}

	accountRendered, err := keppel.RenderAccount(*account)
	if respondwith.ErrorText(w, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, map[string]any{"account": accountRendered})
}

func (a *API) handlePutAccount(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/accounts/:account")
	// decode request body
	var req struct {
		Account keppel.Account `json:"account"`
	}
	ok := decodeJSONRequestBody(w, r.Body, &req)
	if !ok {
		return
	}
	// we do not allow to set name in the request body ...
	if req.Account.Name != "" && req.Account.Name != models.AccountName(mux.Vars(r)["account"]) {
		http.Error(w, `changing attribute "account.name" in request body is not allowed`, http.StatusUnprocessableEntity)
		return
	}
	// ... or state ...
	if req.Account.State != "" {
		http.Error(w, `malformed attribute "account.state" in request body is not allowed here`, http.StatusUnprocessableEntity)
		return
	}
	// ... or metadata ...
	if req.Account.Metadata != nil && len(*req.Account.Metadata) > 0 {
		http.Error(w, `malformed attribute "account.metadata" in request body does no longer exist`, http.StatusUnprocessableEntity)
		return
	}
	// ... and transfer the name here into the struct, to make the below code simpler
	req.Account.Name = models.AccountName(mux.Vars(r)["account"])

	// check permission to create account
	authz := a.authenticateRequest(w, r, authTenantScope(keppel.CanChangeAccount, req.Account.AuthTenantID))
	if authz == nil {
		return
	}

	getSubleaseTokenCallback := func(_ models.Peer) (keppel.SubleaseToken, error) {
		t, err := keppel.ParseSubleaseToken(r.Header.Get(SubleaseHeader))
		if err != nil {
			return keppel.SubleaseToken{}, fmt.Errorf("malformed %s header: %w", SubleaseHeader, err)
		}
		return t, nil
	}
	finalizeAccountCallback := func(account *models.Account) *keppel.RegistryV2Error {
		if account.IsManaged {
			return keppel.ErrDenied.With("cannot manually change configuration of a managed account").WithStatus(http.StatusForbidden)
		}
		return nil
	}
	account, rerr := a.processor().CreateOrUpdateAccount(r.Context(), req.Account, authz.UserIdentity.UserInfo(), r, getSubleaseTokenCallback, finalizeAccountCallback)
	if rerr != nil {
		rerr.WriteAsTextTo(w)
		return
	}

	accountRendered, err := keppel.RenderAccount(account)
	if respondwith.ErrorText(w, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, map[string]any{"account": accountRendered})
}

func (a *API) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/accounts/:account")
	authz := a.authenticateRequest(w, r, accountScopeFromRequest(r, keppel.CanChangeAccount))
	if authz == nil {
		return
	}
	account := a.findAccountFromRequest(w, r, authz)
	if account == nil {
		return
	}

	if account.IsDeleting {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	err := a.processor().MarkAccountForDeletion(*account, keppel.AuditContext{
		UserIdentity: authz.UserIdentity,
		Request:      r,
	})
	if respondwith.ErrorText(w, err) {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handlePostAccountSublease(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/accounts/:account/sublease")
	authz := a.authenticateRequest(w, r, accountScopeFromRequest(r, keppel.CanChangeAccount))
	if authz == nil {
		return
	}
	account := a.findAccountFromRequest(w, r, authz)
	if account == nil {
		return
	}

	if account.UpstreamPeerHostName != "" {
		http.Error(w, "operation not allowed for replica accounts", http.StatusBadRequest)
		return
	}

	st := keppel.SubleaseToken{
		AccountName:     account.Name,
		PrimaryHostname: a.cfg.APIPublicHostname,
	}

	var err error
	st.Secret, err = a.fd.IssueSubleaseTokenSecret(r.Context(), *account)
	if respondwith.ErrorText(w, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, map[string]any{"sublease_token": st.Serialize()})
}

func (a *API) handleGetSecurityScanPolicies(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/accounts/:account/security_scan_policies")
	authz := a.authenticateRequest(w, r, accountScopeFromRequest(r, keppel.CanViewAccount))
	if authz == nil {
		return
	}
	account := a.findAccountFromRequest(w, r, authz)
	if account == nil {
		return
	}

	respondwith.JSON(w, http.StatusOK, map[string]any{"policies": json.RawMessage(account.SecurityScanPoliciesJSON)})
}

func (a *API) handlePutSecurityScanPolicies(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/accounts/:account/security_scan_policies")
	authz := a.authenticateRequest(w, r, accountScopeFromRequest(r, keppel.CanChangeAccount))
	if authz == nil {
		return
	}
	account := a.findAccountFromRequest(w, r, authz)
	if account == nil {
		return
	}

	// decode existing policies
	var dbPolicies []keppel.SecurityScanPolicy
	err := json.Unmarshal([]byte(account.SecurityScanPoliciesJSON), &dbPolicies)
	if respondwith.ErrorText(w, err) {
		return
	}

	// decode request body
	var req struct {
		Policies []keppel.SecurityScanPolicy `json:"policies"`
	}
	ok := decodeJSONRequestBody(w, r.Body, &req)
	if !ok {
		return
	}

	// apply computed values and validate each input policy on its own
	currentUserName := authz.UserIdentity.UserName()
	var errs errext.ErrorSet
	for idx, policy := range req.Policies {
		path := fmt.Sprintf("policies[%d]", idx)
		errs.Append(policy.Validate(path))

		switch policy.ManagingUserName {
		case "$REQUESTER":
			req.Policies[idx].ManagingUserName = currentUserName
		case "", currentUserName:
			// acceptable
		default:
			if !slices.Contains(dbPolicies, policy) {
				errs.Addf("cannot apply this new or updated policy that is managed by a different user: %s", policy)
			}
		}
	}

	// check that updated or deleted policies are either unmanaged or managed by
	// the requester
	for _, dbPolicy := range dbPolicies {
		if slices.Contains(req.Policies, dbPolicy) {
			continue
		}
		managingUserName := dbPolicy.ManagingUserName
		if managingUserName != "" && managingUserName != currentUserName {
			errs.Addf("cannot update or delete this existing policy that is managed by a different user: %s", dbPolicy)
		}
	}

	// report validation errors
	if !errs.IsEmpty() {
		http.Error(w, errs.Join("\n"), http.StatusUnprocessableEntity)
		return
	}

	// update policies in DB
	jsonBuf, err := json.Marshal(req.Policies)
	if respondwith.ErrorText(w, err) {
		return
	}
	_, err = a.db.Exec(`UPDATE accounts SET security_scan_policies_json = $1 WHERE name = $2`,
		string(jsonBuf), account.Name)
	if respondwith.ErrorText(w, err) {
		return
	}

	// generate audit events
	submitAudit := func(action cadf.Action, target audittools.Target) {
		if userInfo := authz.UserIdentity.UserInfo(); userInfo != nil {
			a.auditor.Record(audittools.Event{
				Time:       time.Now(),
				Request:    r,
				User:       userInfo,
				ReasonCode: http.StatusOK,
				Action:     action,
				Target:     target,
			})
		}
	}
	for _, policy := range req.Policies {
		if !slices.Contains(dbPolicies, policy) {
			submitAudit("create/security-scan-policy", AuditSecurityScanPolicy{
				Account: *account,
				Policy:  policy,
			})
		}
	}
	for _, policy := range dbPolicies {
		if !slices.Contains(req.Policies, policy) {
			submitAudit("delete/security-scan-policy", AuditSecurityScanPolicy{
				Account: *account,
				Policy:  policy,
			})
		}
	}

	respondwith.JSON(w, http.StatusOK, map[string]any{"policies": req.Policies})
}
