/******************************************************************************
*
*  Copyright 2018-2020 SAP SE
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

package keppelv1

import (
	"context"
	"database/sql"
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
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

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
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&req)
	if err != nil {
		http.Error(w, "request body is not valid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	// we do not allow to set name in the request body ...
	if req.Account.Name != "" {
		http.Error(w, `malformed attribute "account.name" in request body is not allowed here`, http.StatusUnprocessableEntity)
		return
	}
	// ... transfer it here into the struct, to make the below code simpler
	req.Account.Name = mux.Vars(r)["account"]

	// check permission to create account
	authz := a.authenticateRequest(w, r, authTenantScope(keppel.CanChangeAccount, req.Account.AuthTenantID))
	if authz == nil {
		return
	}

	account, rerr := a.processor().CreateOrUpdateAccount(r.Context(), req.Account, a.fd, authz.UserIdentity.UserInfo(), r, func(_ models.Peer) (string, *keppel.RegistryV2Error) {
		subleaseToken, err := SubleaseTokenFromRequest(r)
		if err != nil {
			return "", keppel.AsRegistryV2Error(err)
		}
		return subleaseToken.Secret, nil
	})
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

type deleteAccountRemainingManifest struct {
	RepositoryName string `json:"repository"`
	Digest         string `json:"digest"`
}

type deleteAccountRemainingManifests struct {
	Count uint64                           `json:"count"`
	Next  []deleteAccountRemainingManifest `json:"next"`
}

type deleteAccountRemainingBlobs struct {
	Count uint64 `json:"count"`
}

type deleteAccountResponse struct {
	RemainingManifests *deleteAccountRemainingManifests `json:"remaining_manifests,omitempty"`
	RemainingBlobs     *deleteAccountRemainingBlobs     `json:"remaining_blobs,omitempty"`
	Error              string                           `json:"error,omitempty"`
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

	resp, err := a.deleteAccount(r.Context(), *account)
	if respondwith.ErrorText(w, err) {
		return
	}
	if resp == nil {
		w.WriteHeader(http.StatusNoContent)
	} else {
		respondwith.JSON(w, http.StatusConflict, resp)
	}
}

var (
	deleteAccountFindManifestsQuery = sqlext.SimplifyWhitespace(`
		SELECT r.name, m.digest
			FROM manifests m
			JOIN repos r ON m.repo_id = r.id
			JOIN accounts a ON a.name = r.account_name
			LEFT OUTER JOIN manifest_manifest_refs mmr ON mmr.repo_id = r.id AND m.digest = mmr.child_digest
		 WHERE a.name = $1 AND parent_digest IS NULL
		 LIMIT 10
	`)
	deleteAccountCountManifestsQuery = sqlext.SimplifyWhitespace(`
		SELECT COUNT(m.digest)
			FROM manifests m
			JOIN repos r ON m.repo_id = r.id
			JOIN accounts a ON a.name = r.account_name
		 WHERE a.name = $1
	`)
	deleteAccountReposQuery                   = `DELETE FROM repos WHERE account_name = $1`
	deleteAccountCountBlobsQuery              = `SELECT COUNT(id) FROM blobs WHERE account_name = $1`
	deleteAccountScheduleBlobSweepQuery       = `UPDATE accounts SET next_blob_sweep_at = $2 WHERE name = $1`
	deleteAccountMarkAllBlobsForDeletionQuery = `UPDATE blobs SET can_be_deleted_at = $2 WHERE account_name = $1`
)

func (a *API) deleteAccount(ctx context.Context, account models.Account) (*deleteAccountResponse, error) {
	if !account.InMaintenance {
		return &deleteAccountResponse{
			Error: "account must be set in maintenance first",
		}, nil
	}

	// can only delete account when user has deleted all manifests from it
	var nextManifests []deleteAccountRemainingManifest
	err := sqlext.ForeachRow(a.db, deleteAccountFindManifestsQuery, []any{account.Name},
		func(rows *sql.Rows) error {
			var m deleteAccountRemainingManifest
			err := rows.Scan(&m.RepositoryName, &m.Digest)
			nextManifests = append(nextManifests, m)
			return err
		},
	)
	if err != nil {
		return nil, err
	}
	if len(nextManifests) > 0 {
		manifestCount, err := a.db.SelectInt(deleteAccountCountManifestsQuery, account.Name)
		return &deleteAccountResponse{
			RemainingManifests: &deleteAccountRemainingManifests{
				Count: uint64(manifestCount),
				Next:  nextManifests,
			},
		}, err
	}

	// delete all repos (and therefore, all blob mounts), so that blob sweeping
	// can immediately take place
	_, err = a.db.Exec(deleteAccountReposQuery, account.Name)
	if err != nil {
		return nil, err
	}

	// can only delete account when all blobs have been deleted
	blobCount, err := a.db.SelectInt(deleteAccountCountBlobsQuery, account.Name)
	if err != nil {
		return nil, err
	}
	if blobCount > 0 {
		// make sure that blob sweep runs immediately
		_, err := a.db.Exec(deleteAccountMarkAllBlobsForDeletionQuery, account.Name, time.Now())
		if err != nil {
			return nil, err
		}
		_, err = a.db.Exec(deleteAccountScheduleBlobSweepQuery, account.Name, time.Now())
		if err != nil {
			return nil, err
		}
		return &deleteAccountResponse{
			RemainingBlobs: &deleteAccountRemainingBlobs{Count: uint64(blobCount)},
		}, nil
	}

	// start deleting the account in a transaction
	tx, err := a.db.Begin()
	if err != nil {
		return nil, err
	}
	defer sqlext.RollbackUnlessCommitted(tx)
	_, err = tx.Delete(&account)
	if err != nil {
		return nil, err
	}

	// before committing the transaction, confirm account deletion with the
	// storage driver and the federation driver
	err = a.sd.CleanupAccount(account)
	if err != nil {
		return &deleteAccountResponse{Error: err.Error()}, nil
	}
	err = a.fd.ForfeitAccountName(ctx, account)
	if err != nil {
		return &deleteAccountResponse{Error: err.Error()}, nil
	}

	return nil, tx.Commit()
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

	st := SubleaseToken{
		AccountName:     account.Name,
		PrimaryHostname: a.cfg.APIPublicHostname,
	}

	var err error
	st.Secret, err = a.fd.IssueSubleaseTokenSecret(r.Context(), *account)
	if respondwith.ErrorText(w, err) {
		return
	}

	// only serialize SubleaseToken if it contains a secret at all
	var serialized string
	if st.Secret == "" {
		serialized = ""
	} else {
		serialized = st.Serialize()
	}

	respondwith.JSON(w, http.StatusOK, map[string]any{"sublease_token": serialized})
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
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		http.Error(w, "request body is not valid JSON: "+err.Error(), http.StatusBadRequest)
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
	submitAudit := func(action cadf.Action, target audittools.TargetRenderer) {
		if userInfo := authz.UserIdentity.UserInfo(); userInfo != nil {
			a.auditor.Record(audittools.EventParameters{
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
