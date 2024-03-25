/******************************************************************************
*
*  Copyright 2024 SAP SE
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

package keppel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/sapcc/keppel/internal/models"

	"github.com/sapcc/go-bits/sqlext"
)

// Account represents an account in the API.
type Account struct {
	Name              string                `json:"name"`
	AuthTenantID      string                `json:"auth_tenant_id"`
	GCPolicies        []GCPolicy            `json:"gc_policies,omitempty"`
	InMaintenance     bool                  `json:"in_maintenance"`
	Metadata          map[string]string     `json:"metadata"`
	RBACPolicies      []RBACPolicy          `json:"rbac_policies"`
	ReplicationPolicy *ReplicationPolicy    `json:"replication,omitempty"`
	ValidationPolicy  *ValidationPolicy     `json:"validation,omitempty"`
	PlatformFilter    models.PlatformFilter `json:"platform_filter,omitempty"`
}

var looksLikeAPIVersionRx = regexp.MustCompile(`^v[0-9][1-9]*$`)

// Like reflect.DeepEqual, but ignores some fields that are allowed to be
// updated after account creation.
func replicationPoliciesFunctionallyEqual(lhs, rhs *ReplicationPolicy) bool {
	// one nil and one non-nil is not equal
	if (lhs == nil) != (rhs == nil) {
		return false
	}
	// two nil's are equal
	if lhs == nil {
		return true
	}

	// ignore pull credentials (the user shall be able to change these after account creation)
	lhsClone := *lhs
	rhsClone := *rhs
	lhsClone.ExternalPeer.UserName = ""
	lhsClone.ExternalPeer.Password = ""
	rhsClone.ExternalPeer.UserName = ""
	rhsClone.ExternalPeer.Password = ""
	return reflect.DeepEqual(lhsClone, rhsClone)
}

func RenderReplicationPolicy(dbAccount models.Account) *ReplicationPolicy {
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

// ValidateAndNormalize can be used on an API account and returns the database representation of it.
func (a *Account) ValidateAndNormalize(authDriver AuthDriver, db *DB, isAuthenticated func(models.Account) (bool, *RegistryV2Error)) (account *models.Account, needsCreation, needsUpdate, needsAudit bool, rerr *RegistryV2Error) {
	err := authDriver.ValidateTenantID(a.AuthTenantID)
	if err != nil {
		return nil, false, false, false, AsRegistryV2Error(fmt.Errorf(`malformed attribute "auth_tenant_id": %w`, err)).WithStatus(http.StatusUnprocessableEntity)
	}

	// reserve identifiers for internal pseudo-accounts and anything that might
	// appear like the first path element of a legal endpoint path on any of our
	// APIs (we will soon start recognizing image-like URLs such as
	// keppel.example.org/account/repo and offer redirection to a suitable UI;
	// this requires the account name to not overlap with API endpoint paths)
	if strings.HasPrefix(a.Name, "keppel") {
		return nil, false, false, false, AsRegistryV2Error(errors.New(`account names with the prefix "keppel" are reserved for internal use`)).WithStatus(http.StatusUnprocessableEntity)
	}
	if looksLikeAPIVersionRx.MatchString(a.Name) {
		return nil, false, false, false, AsRegistryV2Error(errors.New(`account names that look like API versions (eg. v1) are reserved for internal use`)).WithStatus(http.StatusUnprocessableEntity)
	}

	for _, policy := range a.GCPolicies {
		err := policy.Validate()
		if err != nil {
			return nil, false, false, false, AsRegistryV2Error(err).WithStatus(http.StatusUnprocessableEntity)
		}
	}

	for idx, policy := range a.RBACPolicies {
		err := policy.ValidateAndNormalize()
		if err != nil {
			return nil, false, false, false, AsRegistryV2Error(err).WithStatus(http.StatusUnprocessableEntity)
		}
		a.RBACPolicies[idx] = policy
	}

	metadataJSONStr := ""
	if len(a.Metadata) > 0 {
		metadataJSON, _ := json.Marshal(a.Metadata)
		metadataJSONStr = string(metadataJSON)
	}

	gcPoliciesJSONStr := "[]"
	if len(a.GCPolicies) > 0 {
		gcPoliciesJSON, _ := json.Marshal(a.GCPolicies)
		gcPoliciesJSONStr = string(gcPoliciesJSON)
	}

	rbacPoliciesJSONStr := ""
	if len(a.RBACPolicies) > 0 {
		rbacPoliciesJSON, _ := json.Marshal(a.RBACPolicies)
		rbacPoliciesJSONStr = string(rbacPoliciesJSON)
	}

	accountToCreate := models.Account{
		Name:                     a.Name,
		AuthTenantID:             a.AuthTenantID,
		InMaintenance:            a.InMaintenance,
		MetadataJSON:             metadataJSONStr,
		GCPoliciesJSON:           gcPoliciesJSONStr,
		RBACPoliciesJSON:         rbacPoliciesJSONStr,
		SecurityScanPoliciesJSON: "[]",
	}

	// db MUST NOT be used before this point, otherwise we could leak internals without authentication!
	if authed, rerr := isAuthenticated(accountToCreate); !authed {
		return nil, false, false, false, rerr
	}

	// validate replication policy
	if a.ReplicationPolicy != nil {
		rp := *a.ReplicationPolicy

		err := rp.ApplyToAccount(db, &accountToCreate)
		if err != nil {
			return nil, false, false, false, err
		}
		//NOTE: There are some delayed checks below which require the existing account to be loaded from the DB first.
	}

	// validate validation policy
	if a.ValidationPolicy != nil {
		vp := *a.ValidationPolicy
		for _, label := range vp.RequiredLabels {
			if strings.Contains(label, ",") {
				return nil, false, false, false, AsRegistryV2Error(fmt.Errorf(`invalid label name: %q`, label)).WithStatus(http.StatusUnprocessableEntity)
			}
		}

		accountToCreate.RequiredLabels = vp.JoinRequiredLabels()
	}

	// validate platform filter
	if a.PlatformFilter != nil {
		if a.ReplicationPolicy == nil {
			return nil, false, false, false, AsRegistryV2Error(errors.New(`platform filter is only allowed on replica accounts`)).WithStatus(http.StatusUnprocessableEntity)
		}
		accountToCreate.PlatformFilter = a.PlatformFilter
	}

	// check if accountToUpdate already exists
	accountToUpdate, err := FindAccount(db, a.Name)
	if err != nil {
		return nil, false, false, false, AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
	}
	if accountToUpdate != nil {
		if accountToUpdate.AuthTenantID != a.AuthTenantID {
			return nil, false, false, false, AsRegistryV2Error(errors.New(`account name already in use by a different tenant`)).WithStatus(http.StatusConflict)
		}
	}

	// late replication policy validations (could not do these earlier because we
	// did not have `account` yet)
	if a.ReplicationPolicy != nil {
		rp := *a.ReplicationPolicy

		if rp.Strategy == "from_external_on_first_use" {
			// for new accounts, we need either full credentials or none
			if accountToUpdate == nil {
				if (rp.ExternalPeer.UserName == "") != (rp.ExternalPeer.Password == "") {
					return nil, false, false, false, AsRegistryV2Error(errors.New(`need either both username and password or neither for "from_external_on_first_use" replication`)).WithStatus(http.StatusUnprocessableEntity)
				}
			}

			// for existing accounts, having only a username is acceptable if it's unchanged
			// (this case occurs when a client GETs the account, changes something unrelated to replication, and PUTs the result;
			// the password is redacted in GET)
			if accountToUpdate != nil && rp.ExternalPeer.UserName != "" && rp.ExternalPeer.Password == "" {
				if rp.ExternalPeer.UserName == accountToUpdate.ExternalPeerUserName {
					rp.ExternalPeer.Password = accountToUpdate.ExternalPeerPassword // to pass the equality checks below
				} else {
					return nil, false, false, false, AsRegistryV2Error(errors.New(`cannot change username for "from_external_on_first_use" replication without also changing password`)).WithStatus(http.StatusUnprocessableEntity)
				}
			}
		}
	}

	// replication strategy may not be changed after account creation
	if accountToUpdate != nil {
		if a.ReplicationPolicy != nil && !replicationPoliciesFunctionallyEqual(a.ReplicationPolicy, RenderReplicationPolicy(*accountToUpdate)) {
			return nil, false, false, false, AsRegistryV2Error(errors.New(`cannot change replication policy on existing account`)).WithStatus(http.StatusConflict)
		}
		if a.PlatformFilter != nil && !reflect.DeepEqual(a.PlatformFilter, accountToUpdate.PlatformFilter) {
			return nil, false, false, false, AsRegistryV2Error(errors.New(`cannot change platform filter on existing account`)).WithStatus(http.StatusConflict)
		}
	}

	// late RBAC policy validations (could not do these earlier because we did not
	// have `account` yet)
	isExternalReplica := a.ReplicationPolicy != nil && a.ReplicationPolicy.ExternalPeer.URL != ""
	if accountToUpdate != nil {
		isExternalReplica = accountToUpdate.ExternalPeerURL != ""
	}
	for _, policy := range a.RBACPolicies {
		if slices.Contains(policy.Permissions, GrantsAnonymousFirstPull) && !isExternalReplica {
			return nil, false, false, false, AsRegistryV2Error(errors.New(`RBAC policy with "anonymous_first_pull" may only be for external replica accounts`)).WithStatus(http.StatusUnprocessableEntity)
		}
	}

	// TODO: why not always create an audit event?
	if accountToUpdate != nil {
		if accountToUpdate.InMaintenance != accountToCreate.InMaintenance {
			accountToUpdate.InMaintenance = accountToCreate.InMaintenance
			needsUpdate = true
		}
		if accountToUpdate.MetadataJSON != accountToCreate.MetadataJSON {
			accountToUpdate.MetadataJSON = accountToCreate.MetadataJSON
			needsUpdate = true
		}
		if accountToUpdate.GCPoliciesJSON != accountToCreate.GCPoliciesJSON {
			accountToUpdate.GCPoliciesJSON = accountToCreate.GCPoliciesJSON
			needsUpdate = true
			needsAudit = true
		}
		if accountToUpdate.RBACPoliciesJSON != accountToCreate.RBACPoliciesJSON {
			accountToUpdate.RBACPoliciesJSON = accountToCreate.RBACPoliciesJSON
			needsUpdate = true
			needsAudit = true
		}
		if accountToUpdate.RequiredLabels != accountToCreate.RequiredLabels {
			accountToUpdate.RequiredLabels = accountToCreate.RequiredLabels
			needsUpdate = true
		}
		if accountToUpdate.ExternalPeerUserName != accountToCreate.ExternalPeerUserName {
			accountToUpdate.ExternalPeerUserName = accountToCreate.ExternalPeerUserName
			needsUpdate = true
		}
		if accountToUpdate.ExternalPeerPassword != accountToCreate.ExternalPeerPassword {
			accountToUpdate.ExternalPeerPassword = accountToCreate.ExternalPeerPassword
			needsUpdate = true
		}
	}

	if accountToUpdate == nil {
		return &accountToCreate, true, false, false, nil
	}
	return accountToUpdate, false, needsUpdate, needsAudit, nil
}

func CreateAccountInDB(ctx context.Context, db *DB, fd FederationDriver, sd StorageDriver, subleaseHeader string, getUpstreamAccount func(upstreamPeerHostName string) (upstreamAccount Account), account *models.Account) *RegistryV2Error {
	// sublease tokens are only relevant when creating replica accounts
	subleaseTokenSecret := ""
	if account.UpstreamPeerHostName != "" {
		subleaseToken, err := SubleaseTokenFromRequest(subleaseHeader)
		if err != nil {
			return AsRegistryV2Error(err).WithStatus(http.StatusBadRequest)
		}
		subleaseTokenSecret = subleaseToken.Secret
	}

	// check permission to claim account name (this only happens here because
	// it's only relevant for account creations, not for updates)
	claimResult, err := fd.ClaimAccountName(ctx, *account, subleaseTokenSecret)
	switch claimResult {
	case ClaimSucceeded:
		// nothing to do
	case ClaimFailed:
		// user error
		return AsRegistryV2Error(err).WithStatus(http.StatusForbidden)
	case ClaimErrored:
		// server error
		return AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
	}

	// Copy PlatformFilter when creating an account with the Replication Policy on_first_use
	if account.UpstreamPeerHostName != "" {
		upstreamAccount := getUpstreamAccount(account.UpstreamPeerHostName)

		if account.PlatformFilter == nil {
			account.PlatformFilter = upstreamAccount.PlatformFilter
		} else if !reflect.DeepEqual(account.PlatformFilter, upstreamAccount.PlatformFilter) {
			// check if the peer PlatformFilter matches the primary account PlatformFilter
			jsonPlatformFilter, err := json.Marshal(account.PlatformFilter)
			if err != nil {
				return AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
			}
			jsonFilter, err := json.Marshal(upstreamAccount.PlatformFilter)
			if err != nil {
				return AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
			}
			msg := fmt.Sprintf("peer account filter needs to match primary account filter: primary account %s, peer account %s ", jsonPlatformFilter, jsonFilter)
			return AsRegistryV2Error(errors.New(msg)).WithStatus(http.StatusConflict)
		}
	}

	err = sd.CanSetupAccount(*account)
	if err != nil {
		return AsRegistryV2Error(fmt.Errorf("cannot set up backing storage for this account: %w", err)).WithStatus(http.StatusConflict)
	}

	tx, err := db.Begin()
	if err != nil {
		return AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
	}
	defer sqlext.RollbackUnlessCommitted(tx)

	err = tx.Insert(account)
	if err != nil {
		return AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
	}

	// commit the changes
	err = tx.Commit()
	if err != nil {
		return AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
	}

	return nil
}
