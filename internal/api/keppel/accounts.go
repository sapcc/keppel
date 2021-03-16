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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/go-bits/sre"
	"github.com/sapcc/keppel/internal/keppel"
)

////////////////////////////////////////////////////////////////////////////////
// data types

//Account represents an account in the API.
type Account struct {
	Name              string                `json:"name"`
	AuthTenantID      string                `json:"auth_tenant_id"`
	InMaintenance     bool                  `json:"in_maintenance"`
	Metadata          map[string]string     `json:"metadata"`
	RBACPolicies      []RBACPolicy          `json:"rbac_policies"`
	ReplicationPolicy *ReplicationPolicy    `json:"replication,omitempty"`
	ValidationPolicy  *ValidationPolicy     `json:"validation,omitempty"`
	PlatformFilter    keppel.PlatformFilter `json:"platform_filter,omitempty"`
}

//RBACPolicy represents an RBAC policy in the API.
type RBACPolicy struct {
	RepositoryPattern string   `json:"match_repository,omitempty"`
	UserNamePattern   string   `json:"match_username,omitempty"`
	Permissions       []string `json:"permissions"`
}

//ReplicationPolicy represents a replication policy in the API.
type ReplicationPolicy struct {
	Strategy string
	//only for `on_first_use`
	UpstreamPeerHostName string
	//only for `from_external_on_first_use`
	ExternalPeer ReplicationExternalPeerSpec
}

//ReplicationExternalPeerSpec appears in type ReplicationPolicy.
type ReplicationExternalPeerSpec struct {
	URL      string `json:"url"`
	UserName string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

//ValidationPolicy represents a validation policy in the API.
type ValidationPolicy struct {
	RequiredLabels []string `json:"required_labels,omitempty"`
}

//MarshalJSON implements the json.Marshaler interface.
func (r ReplicationPolicy) MarshalJSON() ([]byte, error) {
	switch r.Strategy {
	case "on_first_use":
		data := struct {
			Strategy             string `json:"strategy"`
			UpstreamPeerHostName string `json:"upstream"`
		}{r.Strategy, r.UpstreamPeerHostName}
		return json.Marshal(data)
	case "from_external_on_first_use":
		data := struct {
			Strategy     string                      `json:"strategy"`
			ExternalPeer ReplicationExternalPeerSpec `json:"upstream"`
		}{r.Strategy, r.ExternalPeer}
		return json.Marshal(data)
	default:
		return nil, fmt.Errorf("do not know how to serialize ReplicationPolicy with strategy %q", r.Strategy)
	}
}

//UnmarshalJSON implements the json.Unmarshaler interface.
func (r *ReplicationPolicy) UnmarshalJSON(buf []byte) error {
	var s struct {
		Strategy string          `json:"strategy"`
		Upstream json.RawMessage `json:"upstream"`
	}
	err := json.Unmarshal(buf, &s)
	if err != nil {
		return err
	}
	r.Strategy = s.Strategy

	switch r.Strategy {
	case "on_first_use":
		return json.Unmarshal(s.Upstream, &r.UpstreamPeerHostName)
	case "from_external_on_first_use":
		return json.Unmarshal(s.Upstream, &r.ExternalPeer)
	default:
		return fmt.Errorf("do not know how to deserialize ReplicationPolicy with strategy %q", r.Strategy)
	}
}

////////////////////////////////////////////////////////////////////////////////
// data conversion/validation functions

func (a *API) renderAccount(dbAccount keppel.Account) (Account, error) {
	var dbPolicies []keppel.RBACPolicy
	_, err := a.db.Select(&dbPolicies, `SELECT * FROM rbac_policies WHERE account_name = $1`, dbAccount.Name)
	if err != nil {
		return Account{}, err
	}

	policies := make([]RBACPolicy, len(dbPolicies))
	for idx, p := range dbPolicies {
		policies[idx] = renderRBACPolicy(p)
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
		InMaintenance:     dbAccount.InMaintenance,
		Metadata:          metadata,
		RBACPolicies:      policies,
		ReplicationPolicy: renderReplicationPolicy(dbAccount),
		ValidationPolicy:  renderValidationPolicy(dbAccount),
		PlatformFilter:    dbAccount.PlatformFilter,
	}, nil
}

func renderReplicationPolicy(dbAccount keppel.Account) *ReplicationPolicy {
	if dbAccount.UpstreamPeerHostName != "" {
		return &ReplicationPolicy{
			Strategy:             "on_first_use",
			UpstreamPeerHostName: dbAccount.UpstreamPeerHostName,
		}
	}

	if dbAccount.ExternalPeerURL != "" {
		return &ReplicationPolicy{
			Strategy: "from_external_on_first_use",
			ExternalPeer: ReplicationExternalPeerSpec{
				URL:      dbAccount.ExternalPeerURL,
				UserName: dbAccount.ExternalPeerUserName,
				//NOTE: Password is omitted here for security reasons
			},
		}
	}

	return nil
}

func renderValidationPolicy(dbAccount keppel.Account) *ValidationPolicy {
	if dbAccount.RequiredLabels == "" {
		return nil
	}

	return &ValidationPolicy{
		RequiredLabels: strings.Split(dbAccount.RequiredLabels, ","),
	}
}

func renderRBACPolicy(dbPolicy keppel.RBACPolicy) RBACPolicy {
	result := RBACPolicy{
		RepositoryPattern: dbPolicy.RepositoryPattern,
		UserNamePattern:   dbPolicy.UserNamePattern,
	}
	if dbPolicy.CanPullAnonymously {
		result.Permissions = append(result.Permissions, "anonymous_pull")
	}
	if dbPolicy.CanPull {
		result.Permissions = append(result.Permissions, "pull")
	}
	if dbPolicy.CanPush {
		result.Permissions = append(result.Permissions, "push")
	}
	if dbPolicy.CanDelete {
		result.Permissions = append(result.Permissions, "delete")
	}
	return result
}

func renderRBACPolicyPtr(dbPolicy keppel.RBACPolicy) *RBACPolicy {
	policy := renderRBACPolicy(dbPolicy)
	return &policy
}

func parseRBACPolicy(policy RBACPolicy) (keppel.RBACPolicy, error) {
	result := keppel.RBACPolicy{
		RepositoryPattern: policy.RepositoryPattern,
		UserNamePattern:   policy.UserNamePattern,
	}
	for _, perm := range policy.Permissions {
		switch perm {
		case "anonymous_pull":
			result.CanPullAnonymously = true
		case "pull":
			result.CanPull = true
		case "push":
			result.CanPush = true
		case "delete":
			result.CanDelete = true
		default:
			return result, fmt.Errorf("%q is not a valid RBAC policy permission", perm)
		}
	}

	if len(policy.Permissions) == 0 {
		return result, errors.New(`RBAC policy must grant at least one permission`)
	}
	if result.UserNamePattern == "" && result.RepositoryPattern == "" {
		return result, errors.New(`RBAC policy must have at least one "match_..." attribute`)
	}
	if result.CanPullAnonymously && result.UserNamePattern != "" {
		return result, errors.New(`RBAC policy with "anonymous_pull" may not have the "match_username" attribute`)
	}
	if result.CanPull && result.UserNamePattern == "" {
		return result, errors.New(`RBAC policy with "pull" must have the "match_username" attribute`)
	}
	if result.CanPush && !result.CanPull {
		return result, errors.New(`RBAC policy with "push" must also grant "pull"`)
	}
	if result.CanDelete && result.UserNamePattern == "" {
		return result, errors.New(`RBAC policy with "delete" must have the "match_username" attribute`)
	}

	for _, pattern := range []string{policy.RepositoryPattern, policy.UserNamePattern} {
		if pattern == "" {
			continue
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return result, fmt.Errorf("%q is not a valid regex: %s", pattern, err.Error())
		}
	}

	return result, nil
}

////////////////////////////////////////////////////////////////////////////////
// handlers

func (a *API) handleGetAccounts(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/keppel/v1/accounts")
	authz, authErr := a.authDriver.AuthenticateUserFromRequest(r)
	if respondWithAuthError(w, authErr) {
		return
	}

	var accounts []keppel.Account
	_, err := a.db.Select(&accounts, "SELECT * FROM accounts ORDER BY name")
	if respondwith.ErrorText(w, err) {
		return
	}

	//restrict accounts to those visible in the current scope
	var accountsFiltered []keppel.Account
	for _, account := range accounts {
		if authz.HasPermission(keppel.CanViewAccount, account.AuthTenantID) {
			accountsFiltered = append(accountsFiltered, account)
		}
	}
	//ensure that this serializes as a list, not as null
	if len(accountsFiltered) == 0 {
		accountsFiltered = []keppel.Account{}
	}

	//render accounts to JSON
	accountsRendered := make([]Account, len(accountsFiltered))
	for idx, account := range accountsFiltered {
		accountsRendered[idx], err = a.renderAccount(account)
		if respondwith.ErrorText(w, err) {
			return
		}
	}
	respondwith.JSON(w, http.StatusOK, map[string]interface{}{"accounts": accountsRendered})
}

func (a *API) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/keppel/v1/accounts/:account")
	account, _ := a.authenticateAccountScopedRequest(w, r, keppel.CanViewAccount)
	if account == nil {
		return
	}

	accountRendered, err := a.renderAccount(*account)
	if respondwith.ErrorText(w, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, map[string]interface{}{"account": accountRendered})
}

var looksLikeAPIVersionRx = regexp.MustCompile(`^v[0-9][1-9]*$`)

func (a *API) handlePutAccount(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/keppel/v1/accounts/:account")
	//decode request body
	var req struct {
		Account struct {
			AuthTenantID      string                `json:"auth_tenant_id"`
			InMaintenance     bool                  `json:"in_maintenance"`
			Metadata          map[string]string     `json:"metadata"`
			RBACPolicies      []RBACPolicy          `json:"rbac_policies"`
			ReplicationPolicy *ReplicationPolicy    `json:"replication"`
			ValidationPolicy  *ValidationPolicy     `json:"validation"`
			PlatformFilter    keppel.PlatformFilter `json:"platform_filter"`
		} `json:"account"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&req)
	if err != nil {
		http.Error(w, "request body is not valid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.authDriver.ValidateTenantID(req.Account.AuthTenantID); err != nil {
		http.Error(w, `malformed attribute "account.auth_tenant_id" in request body: `+err.Error(), http.StatusUnprocessableEntity)
		return
	}

	//reserve identifiers for internal pseudo-accounts and anything that might
	//appear like the first path element of a legal endpoint path on any of our
	//APIs (we will soon start recognizing image-like URLs such as
	//keppel.example.org/account/repo and offer redirection to a suitable UI;
	//this requires the account name to not overlap with API endpoint paths)
	accountName := mux.Vars(r)["account"]
	if strings.HasPrefix(accountName, "keppel") {
		http.Error(w, `account names with the prefix "keppel" are reserved for internal use`, http.StatusUnprocessableEntity)
		return
	}
	if looksLikeAPIVersionRx.MatchString(accountName) {
		http.Error(w, `account names that look like API versions are reserved for internal use`, http.StatusUnprocessableEntity)
		return
	}

	rbacPolicies := make([]keppel.RBACPolicy, len(req.Account.RBACPolicies))
	for idx, policy := range req.Account.RBACPolicies {
		rbacPolicies[idx], err = parseRBACPolicy(policy)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
	}

	metadataJSONStr := ""
	if len(req.Account.Metadata) > 0 {
		metadataJSON, _ := json.Marshal(req.Account.Metadata)
		metadataJSONStr = string(metadataJSON)
	}

	accountToCreate := keppel.Account{
		Name:           accountName,
		AuthTenantID:   req.Account.AuthTenantID,
		InMaintenance:  req.Account.InMaintenance,
		MetadataJSON:   metadataJSONStr,
		GCPoliciesJSON: "[]",
	}

	//validate replication policy
	if req.Account.ReplicationPolicy != nil {
		rp := *req.Account.ReplicationPolicy

		switch rp.Strategy {
		case "on_first_use":
			peerCount, err := a.db.SelectInt(`SELECT COUNT(*) FROM peers WHERE hostname = $1`, rp.UpstreamPeerHostName)
			if respondwith.ErrorText(w, err) {
				return
			}
			if peerCount == 0 {
				http.Error(w, fmt.Sprintf(`unknown peer registry: %q`, rp.UpstreamPeerHostName), http.StatusUnprocessableEntity)
				return
			}
			accountToCreate.UpstreamPeerHostName = rp.UpstreamPeerHostName
		case "from_external_on_first_use":
			if rp.ExternalPeer.URL == "" {
				http.Error(w, `missing upstream URL for "from_external_on_first_use" replication`, http.StatusUnprocessableEntity)
				return
			}
			if (rp.ExternalPeer.UserName == "") != (rp.ExternalPeer.Password == "") {
				http.Error(w, `need either both username and password or neither for "from_external_on_first_use" replication`, http.StatusUnprocessableEntity)
				return
			}
			accountToCreate.ExternalPeerURL = rp.ExternalPeer.URL
			accountToCreate.ExternalPeerUserName = rp.ExternalPeer.UserName
			accountToCreate.ExternalPeerPassword = rp.ExternalPeer.Password
		}
	}

	//validate validation policy
	if req.Account.ValidationPolicy != nil {
		vp := *req.Account.ValidationPolicy
		for _, label := range vp.RequiredLabels {
			if strings.Contains(label, ",") {
				http.Error(w, fmt.Sprintf(`invalid label name: %q`, label), http.StatusUnprocessableEntity)
				return
			}
		}

		accountToCreate.RequiredLabels = strings.Join(vp.RequiredLabels, ",")
	}

	//validate platform filter
	if req.Account.PlatformFilter != nil {
		if req.Account.ReplicationPolicy == nil {
			http.Error(w, `platform filter is only allowed on replica accounts`, http.StatusUnprocessableEntity)
			return
		}
		accountToCreate.PlatformFilter = req.Account.PlatformFilter
	}

	//check permission to create account
	authz, authErr := a.authDriver.AuthenticateUserFromRequest(r)
	if respondWithAuthError(w, authErr) {
		return
	}
	if !authz.HasPermission(keppel.CanChangeAccount, accountToCreate.AuthTenantID) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	//check if account already exists
	account, err := keppel.FindAccount(a.db, accountName)
	if respondwith.ErrorText(w, err) {
		return
	}
	if account != nil && account.AuthTenantID != req.Account.AuthTenantID {
		http.Error(w, `account name already in use by a different tenant`, http.StatusConflict)
		return
	}

	//replication strategy may not be changed after account creation
	if account != nil && req.Account.ReplicationPolicy != nil && !replicationPoliciesFunctionallyEqual(req.Account.ReplicationPolicy, renderReplicationPolicy(*account)) {
		http.Error(w, `cannot change replication policy on existing account`, http.StatusConflict)
		return
	}
	if account != nil && req.Account.PlatformFilter != nil && !reflect.DeepEqual(req.Account.PlatformFilter, account.PlatformFilter) {
		http.Error(w, `cannot change platform filter on existing account`, http.StatusConflict)
		return
	}

	//create account if required
	if account == nil {
		//sublease tokens are only relevant when creating replica accounts
		subleaseTokenSecret := ""
		if accountToCreate.UpstreamPeerHostName != "" {
			subleaseToken, err := SubleaseTokenFromRequest(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			subleaseTokenSecret = subleaseToken.Secret
		}

		//check permission to claim account name (this only happens here because
		//it's only relevant for account creations, not for updates)
		claimResult, err := a.fd.ClaimAccountName(accountToCreate, authz, subleaseTokenSecret)
		switch claimResult {
		case keppel.ClaimSucceeded:
			//nothing to do
		case keppel.ClaimFailed:
			//user error
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		case keppel.ClaimErrored:
			//server error
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tx, err := a.db.Begin()
		if respondwith.ErrorText(w, err) {
			return
		}
		defer keppel.RollbackUnlessCommitted(tx)

		account = &accountToCreate
		err = tx.Insert(account)
		if respondwith.ErrorText(w, err) {
			return
		}

		//before committing this, add the required role assignments
		err = a.authDriver.SetupAccount(*account, authz)
		if respondwith.ErrorText(w, err) {
			return
		}
		err = tx.Commit()
		if respondwith.ErrorText(w, err) {
			return
		}
		if userInfo := authz.UserInfo(); userInfo != nil {
			a.auditor.Record(audittools.EventParameters{
				Time:       time.Now(),
				Request:    r,
				User:       userInfo,
				ReasonCode: http.StatusOK,
				Action:     "create",
				Target:     AuditAccount{Account: *account},
			})
		}
	} else {
		//account != nil: update if necessary
		needsUpdate := false
		if account.InMaintenance != accountToCreate.InMaintenance {
			account.InMaintenance = accountToCreate.InMaintenance
			needsUpdate = true
		}
		if account.MetadataJSON != accountToCreate.MetadataJSON {
			account.MetadataJSON = accountToCreate.MetadataJSON
			needsUpdate = true
		}
		if account.RequiredLabels != accountToCreate.RequiredLabels {
			account.RequiredLabels = accountToCreate.RequiredLabels
			needsUpdate = true
		}
		if account.ExternalPeerUserName != accountToCreate.ExternalPeerUserName {
			account.ExternalPeerUserName = accountToCreate.ExternalPeerUserName
			needsUpdate = true
		}
		if account.ExternalPeerPassword != accountToCreate.ExternalPeerPassword {
			account.ExternalPeerPassword = accountToCreate.ExternalPeerPassword
			needsUpdate = true
		}
		if needsUpdate {
			_, err := a.db.Update(account)
			if respondwith.ErrorText(w, err) {
				return
			}
		}
	}

	submitAudit := func(action string, target AuditRBACPolicy) {
		if userInfo := authz.UserInfo(); userInfo != nil {
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

	for idx, policy := range rbacPolicies {
		policy.AccountName = account.Name
		rbacPolicies[idx] = policy
	}
	err = a.putRBACPolicies(*account, rbacPolicies, submitAudit)
	if respondwith.ErrorText(w, err) {
		return
	}

	accountRendered, err := a.renderAccount(*account)
	if respondwith.ErrorText(w, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, map[string]interface{}{"account": accountRendered})
}

//Like reflect.DeepEqual, but ignores some fields that are allowed to be
//updated after account creation.
func replicationPoliciesFunctionallyEqual(lhs *ReplicationPolicy, rhs *ReplicationPolicy) bool {
	//one nil and one non-nil is not equal
	if (lhs == nil) != (rhs == nil) {
		return false
	}
	//two nil's are equal
	if lhs == nil {
		return true
	}

	//ignore pull credentials (the user shall be able to change these after account creation)
	lhsClone := *lhs
	rhsClone := *rhs
	lhsClone.ExternalPeer.UserName = ""
	lhsClone.ExternalPeer.Password = ""
	rhsClone.ExternalPeer.UserName = ""
	rhsClone.ExternalPeer.Password = ""
	return reflect.DeepEqual(lhsClone, rhsClone)
}

func (a *API) putRBACPolicies(account keppel.Account, policies []keppel.RBACPolicy, submitAudit func(action string, target AuditRBACPolicy)) error {
	//enumerate existing policies
	var dbPolicies []keppel.RBACPolicy
	_, err := a.db.Select(&dbPolicies, `SELECT * FROM rbac_policies WHERE account_name = $1`, account.Name)
	if err != nil {
		return err
	}

	//put existing set of policies in a map to allow diff with new set
	mapKey := func(p keppel.RBACPolicy) string {
		//this mapping is collision-free because RepositoryPattern and UserNamePattern are valid regexes
		return fmt.Sprintf("%s[%s][%s]", p.AccountName, p.RepositoryPattern, p.UserNamePattern)
	}
	state := make(map[string]keppel.RBACPolicy)
	for _, policy := range dbPolicies {
		state[mapKey(policy)] = policy
	}

	//insert or update policies as needed
	for _, policy := range policies {
		key := mapKey(policy)
		if policyInDB, exists := state[key]; exists {
			//update if necessary
			if policy != policyInDB {
				_, err := a.db.Update(&policy)
				if err != nil {
					return err
				}
				submitAudit("update/rbac-policy", AuditRBACPolicy{
					Account: account,
					Before:  renderRBACPolicyPtr(policyInDB),
					After:   renderRBACPolicyPtr(policy),
				})
			}
		} else {
			//insert missing policy
			err := a.db.Insert(&policy)
			if err != nil {
				return err
			}
			submitAudit("create/rbac-policy", AuditRBACPolicy{
				Account: account,
				After:   renderRBACPolicyPtr(policy),
			})
		}

		//remove all updated policies from `state`
		delete(state, key)
	}

	//because of delete() up there, `state` now only contains policies that are
	//not in `policies` and which have to be deleted
	for _, policy := range state {
		_, err := a.db.Delete(&policy)
		if err != nil {
			return err
		}
		submitAudit("delete/rbac-policy", AuditRBACPolicy{
			Account: account,
			Before:  renderRBACPolicyPtr(policy),
		})
	}

	return nil
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
	sre.IdentifyEndpoint(r, "/keppel/v1/accounts/:account")
	account, _ := a.authenticateAccountScopedRequest(w, r, keppel.CanChangeAccount)
	if account == nil {
		return
	}

	resp, err := a.deleteAccount(*account)
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
	deleteAccountFindManifestsQuery = keppel.SimplifyWhitespaceInSQL(`
		SELECT r.name, m.digest
			FROM manifests m
			JOIN repos r ON m.repo_id = r.id
			JOIN accounts a ON a.name = r.account_name
		 WHERE a.name = $1
		 LIMIT 10
	`)
	deleteAccountCountManifestsQuery = keppel.SimplifyWhitespaceInSQL(`
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

func (a *API) deleteAccount(account keppel.Account) (*deleteAccountResponse, error) {
	if !account.InMaintenance {
		return &deleteAccountResponse{
			Error: "account must be set in maintenance first",
		}, nil
	}

	//can only delete account when user has deleted all manifests from it
	var nextManifests []deleteAccountRemainingManifest
	err := keppel.ForeachRow(a.db, deleteAccountFindManifestsQuery, []interface{}{account.Name},
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

	//delete all repos (and therefore, all blob mounts), so that blob sweeping
	//can immediately take place
	_, err = a.db.Exec(deleteAccountReposQuery, account.Name)
	if err != nil {
		return nil, err
	}

	//can only delete account when all blobs have been deleted
	blobCount, err := a.db.SelectInt(deleteAccountCountBlobsQuery, account.Name)
	if err != nil {
		return nil, err
	}
	if blobCount > 0 {
		//make sure that blob sweep runs immediately
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

	//start deleting the account in a transaction
	tx, err := a.db.Begin()
	if err != nil {
		return nil, err
	}
	defer keppel.RollbackUnlessCommitted(tx)
	_, err = tx.Delete(&account)
	if err != nil {
		return nil, err
	}

	//before committing the transaction, confirm account deletion with the federation driver
	err = a.fd.ForfeitAccountName(account)
	if err != nil {
		return &deleteAccountResponse{Error: err.Error()}, nil
	}

	return nil, tx.Commit()
}

func (a *API) handlePostAccountSublease(w http.ResponseWriter, r *http.Request) {
	sre.IdentifyEndpoint(r, "/keppel/v1/accounts/:account/sublease")
	account, _ := a.authenticateAccountScopedRequest(w, r, keppel.CanChangeAccount)
	if account == nil {
		return
	}

	if account.UpstreamPeerHostName != "" {
		http.Error(w, "operation not allowed for replica accounts", http.StatusBadRequest)
		return
	}

	st := SubleaseToken{
		AccountName:     account.Name,
		PrimaryHostname: a.cfg.APIPublicURL.Hostname(),
	}

	var err error
	st.Secret, err = a.fd.IssueSubleaseTokenSecret(*account)
	if respondwith.ErrorText(w, err) {
		return
	}

	//only serialize SubleaseToken if it contains a secret at all
	var serialized string
	if st.Secret == "" {
		serialized = ""
	} else {
		serialized = st.Serialize()
	}

	respondwith.JSON(w, http.StatusOK, map[string]interface{}{"sublease_token": serialized})
}
