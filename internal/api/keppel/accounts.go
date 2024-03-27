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
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strings"
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

////////////////////////////////////////////////////////////////////////////////
// data types

// Account represents an account in the API.
type Account struct {
	Name              string                    `json:"name"`
	AuthTenantID      string                    `json:"auth_tenant_id"`
	GCPolicies        []keppel.GCPolicy         `json:"gc_policies,omitempty"`
	InMaintenance     bool                      `json:"in_maintenance"`
	Metadata          map[string]string         `json:"metadata"`
	RBACPolicies      []keppel.RBACPolicy       `json:"rbac_policies"`
	ReplicationPolicy *keppel.ReplicationPolicy `json:"replication,omitempty"`
	ValidationPolicy  *keppel.ValidationPolicy  `json:"validation,omitempty"`
	PlatformFilter    models.PlatformFilter     `json:"platform_filter,omitempty"`
}

////////////////////////////////////////////////////////////////////////////////
// data conversion/validation functions

func (a *API) renderAccount(dbAccount models.Account) (Account, error) {
	gcPolicies, err := keppel.ParseGCPolicies(dbAccount)
	if err != nil {
		return Account{}, err
	}
	rbacPolicies, err := keppel.ParseRBACPolicies(dbAccount)
	if err != nil {
		return Account{}, err
	}
	if rbacPolicies == nil {
		// do not render "null" in this field
		rbacPolicies = []keppel.RBACPolicy{}
	}

	metadata := make(map[string]string)
	if dbAccount.MetadataJSON != "" {
		err := json.Unmarshal([]byte(dbAccount.MetadataJSON), &metadata)
		if err != nil {
			return Account{}, fmt.Errorf("malformed metadata JSON: %q", dbAccount.MetadataJSON)
		}
	}

	return Account{
		Name:              dbAccount.Name,
		AuthTenantID:      dbAccount.AuthTenantID,
		GCPolicies:        gcPolicies,
		InMaintenance:     dbAccount.InMaintenance,
		Metadata:          metadata,
		RBACPolicies:      rbacPolicies,
		ReplicationPolicy: keppel.RenderReplicationPolicy(dbAccount),
		ValidationPolicy:  keppel.RenderValidationPolicy(dbAccount),
		PlatformFilter:    dbAccount.PlatformFilter,
	}, nil
}

////////////////////////////////////////////////////////////////////////////////
// handlers

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
	accountsRendered := make([]Account, len(accountsFiltered))
	for idx, account := range accountsFiltered {
		accountsRendered[idx], err = a.renderAccount(account)
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

	accountRendered, err := a.renderAccount(*account)
	if respondwith.ErrorText(w, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, map[string]any{"account": accountRendered})
}

var looksLikeAPIVersionRx = regexp.MustCompile(`^v[0-9][1-9]*$`)

func (a *API) handlePutAccount(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/accounts/:account")
	// decode request body
	var req struct {
		Account Account `json:"account"`
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

	// reserve identifiers for internal pseudo-accounts and anything that might
	// appear like the first path element of a legal endpoint path on any of our
	// APIs (we will soon start recognizing image-like URLs such as
	// keppel.example.org/account/repo and offer redirection to a suitable UI;
	// this requires the account name to not overlap with API endpoint paths)
	if strings.HasPrefix(req.Account.Name, "keppel") {
		http.Error(w, `account names with the prefix "keppel" are reserved for internal use`, http.StatusUnprocessableEntity)
		return
	}
	if looksLikeAPIVersionRx.MatchString(req.Account.Name) {
		http.Error(w, `account names that look like API versions are reserved for internal use`, http.StatusUnprocessableEntity)
		return
	}

	// check if account already exists
	originalAccount, err := keppel.FindAccount(a.db, req.Account.Name)
	if respondwith.ErrorText(w, err) {
		return
	}
	if originalAccount != nil && originalAccount.AuthTenantID != req.Account.AuthTenantID {
		http.Error(w, `account name already in use by a different tenant`, http.StatusConflict)
		return
	}

	// PUT can either create a new account or update an existing account;
	// this distinction is important because several fields can only be set at creation
	var targetAccount models.Account
	if originalAccount == nil {
		targetAccount = models.Account{
			Name:                     req.Account.Name,
			AuthTenantID:             req.Account.AuthTenantID,
			SecurityScanPoliciesJSON: "[]",
			// all other attributes are set below or in the ApplyToAccount() methods called below
		}
	} else {
		targetAccount = *originalAccount
	}

	// validate and update fields as requested
	targetAccount.InMaintenance = req.Account.InMaintenance

	// validate GC policies
	if len(req.Account.GCPolicies) == 0 {
		targetAccount.GCPoliciesJSON = "[]"
	} else {
		for _, policy := range req.Account.GCPolicies {
			err := policy.Validate()
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnprocessableEntity)
				return
			}
		}
		buf, _ := json.Marshal(req.Account.GCPolicies)
		targetAccount.GCPoliciesJSON = string(buf)
	}

	// serialize metadata
	if len(req.Account.Metadata) == 0 {
		targetAccount.MetadataJSON = ""
	} else {
		buf, _ := json.Marshal(req.Account.Metadata)
		targetAccount.MetadataJSON = string(buf)
	}

	// validate replication policy (for OnFirstUseStrategy, the peer hostname is
	// checked for correctness down below when validating the platform filter)
	var originalStrategy keppel.ReplicationStrategy
	if originalAccount != nil {
		rp := keppel.RenderReplicationPolicy(*originalAccount)
		if rp == nil {
			originalStrategy = keppel.NoReplicationStrategy
		} else {
			originalStrategy = rp.Strategy
		}
	}

	var replicationStrategy keppel.ReplicationStrategy
	if req.Account.ReplicationPolicy == nil {
		if originalAccount == nil {
			replicationStrategy = keppel.NoReplicationStrategy
		} else {
			// PUT on existing account can omit replication policy to reuse existing policy
			replicationStrategy = originalStrategy
		}
	} else {
		// on existing accounts, we do not allow changing the strategy
		rp := *req.Account.ReplicationPolicy
		if originalAccount != nil && originalStrategy != rp.Strategy {
			http.Error(w, keppel.ErrIncompatibleReplicationPolicy.Error(), http.StatusConflict)
			return
		}

		err := rp.ApplyToAccount(&targetAccount)
		if errors.Is(err, keppel.ErrIncompatibleReplicationPolicy) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		replicationStrategy = rp.Strategy
	}

	// validate RBAC policies
	if len(req.Account.RBACPolicies) == 0 {
		targetAccount.RBACPoliciesJSON = ""
	} else {
		for idx, policy := range req.Account.RBACPolicies {
			err := policy.ValidateAndNormalize(replicationStrategy)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnprocessableEntity)
				return
			}
			req.Account.RBACPolicies[idx] = policy
		}
		buf, _ := json.Marshal(req.Account.RBACPolicies)
		targetAccount.RBACPoliciesJSON = string(buf)
	}

	// validate validation policy
	if req.Account.ValidationPolicy != nil {
		rerr := req.Account.ValidationPolicy.ApplyToAccount(&targetAccount)
		if rerr != nil {
			rerr.WriteAsTextTo(w)
			return
		}
	}

	// validate platform filter
	if originalAccount != nil {
		if req.Account.PlatformFilter != nil && !originalAccount.PlatformFilter.IsEqualTo(req.Account.PlatformFilter) {
			http.Error(w, `cannot change platform filter on existing account`, http.StatusConflict)
			return
		}
	} else {
		switch replicationStrategy {
		case keppel.NoReplicationStrategy:
			if req.Account.PlatformFilter != nil {
				http.Error(w, `platform filter is only allowed on replica accounts`, http.StatusUnprocessableEntity)
				return
			}
		case keppel.FromExternalOnFirstUseStrategy:
			targetAccount.PlatformFilter = req.Account.PlatformFilter
		case keppel.OnFirstUseStrategy:
			// for internal replica accounts, the platform filter must match that of the primary account,
			// either by specifying the same filter explicitly or omitting it
			//
			// NOTE: This validates UpstreamPeerHostName as a side effect.
			upstreamPlatformFilter, err := a.processor().GetPlatformFilterFromPrimaryAccount(r.Context(), targetAccount)
			if errors.Is(err, sql.ErrNoRows) {
				msg := fmt.Sprintf(`unknown peer registry: %q`, targetAccount.UpstreamPeerHostName)
				http.Error(w, msg, http.StatusUnprocessableEntity)
				return
			}
			if respondwith.ErrorText(w, err) {
				return
			}

			if req.Account.PlatformFilter != nil && !upstreamPlatformFilter.IsEqualTo(req.Account.PlatformFilter) {
				jsonPlatformFilter, _ := json.Marshal(req.Account.PlatformFilter)
				jsonFilter, _ := json.Marshal(upstreamPlatformFilter)
				msg := fmt.Sprintf(
					"peer account filter needs to match primary account filter: local account %s, peer account %s ",
					jsonPlatformFilter, jsonFilter)
				http.Error(w, msg, http.StatusConflict)
				return
			}
			targetAccount.PlatformFilter = upstreamPlatformFilter
		}
	}

	// create account if required
	if originalAccount == nil {
		// sublease tokens are only relevant when creating replica accounts
		subleaseTokenSecret := ""
		if targetAccount.UpstreamPeerHostName != "" {
			subleaseToken, err := SubleaseTokenFromRequest(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			subleaseTokenSecret = subleaseToken.Secret
		}

		// check permission to claim account name (this only happens here because
		// it's only relevant for account creations, not for updates)
		claimResult, err := a.fd.ClaimAccountName(r.Context(), targetAccount, subleaseTokenSecret)
		switch claimResult {
		case keppel.ClaimSucceeded:
			// nothing to do
		case keppel.ClaimFailed:
			// user error
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		case keppel.ClaimErrored:
			// server error
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		err = a.sd.CanSetupAccount(targetAccount)
		if err != nil {
			msg := "cannot set up backing storage for this account: " + err.Error()
			http.Error(w, msg, http.StatusConflict)
			return
		}

		tx, err := a.db.Begin()
		if respondwith.ErrorText(w, err) {
			return
		}
		defer sqlext.RollbackUnlessCommitted(tx)

		err = tx.Insert(&targetAccount)
		if respondwith.ErrorText(w, err) {
			return
		}

		// commit the changes
		err = tx.Commit()
		if respondwith.ErrorText(w, err) {
			return
		}
		if userInfo := authz.UserIdentity.UserInfo(); userInfo != nil {
			a.auditor.Record(audittools.EventParameters{
				Time:       time.Now(),
				Request:    r,
				User:       userInfo,
				ReasonCode: http.StatusOK,
				Action:     cadf.CreateAction,
				Target:     AuditAccount{Account: targetAccount},
			})
		}
	} else {
		// originalAccount != nil: update if necessary
		if !reflect.DeepEqual(*originalAccount, targetAccount) {
			_, err := a.db.Update(&targetAccount)
			if respondwith.ErrorText(w, err) {
				return
			}
		}

		// audit log is necessary for all changes except to InMaintenance
		if userInfo := authz.UserIdentity.UserInfo(); userInfo != nil {
			originalAccount.InMaintenance = targetAccount.InMaintenance
			if !reflect.DeepEqual(*originalAccount, targetAccount) {
				a.auditor.Record(audittools.EventParameters{
					Time:       time.Now(),
					Request:    r,
					User:       userInfo,
					ReasonCode: http.StatusOK,
					Action:     cadf.UpdateAction,
					Target:     AuditAccount{Account: targetAccount},
				})
			}
		}
	}

	accountRendered, err := a.renderAccount(targetAccount)
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
