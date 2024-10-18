/*******************************************************************************
*
* Copyright 2024 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package processor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"

	"github.com/sapcc/keppel/internal/auth"
	peerclient "github.com/sapcc/keppel/internal/client/peer"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"

	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/sqlext"
)

// GetPlatformFilterFromPrimaryAccount takes a replica account and queries the peer holding the primary account for that account.
func (p *Processor) GetPlatformFilterFromPrimaryAccount(ctx context.Context, peer models.Peer, replicaAccount models.Account) (models.PlatformFilter, error) {
	viewScope := auth.Scope{
		ResourceType: "keppel_account",
		ResourceName: string(replicaAccount.Name),
		Actions:      []string{"view"},
	}
	client, err := peerclient.New(ctx, p.cfg, peer, viewScope)
	if err != nil {
		return nil, err
	}

	var upstreamAccount keppel.Account
	err = client.GetForeignAccountConfigurationInto(ctx, &upstreamAccount, replicaAccount.Name)
	if err != nil {
		return nil, err
	}
	return upstreamAccount.PlatformFilter, nil
}

var looksLikeAPIVersionRx = regexp.MustCompile(`^v[0-9][1-9]*$`)
var ErrAccountNameEmpty = errors.New("account name cannot be empty string")

// CreateOrUpdate can be used on an API account and returns the database representation of it.
func (p *Processor) CreateOrUpdateAccount(ctx context.Context, account keppel.Account, userInfo audittools.UserInfo, r *http.Request, getSubleaseToken func(models.Peer) (string, *keppel.RegistryV2Error), setCustomFields func(*models.Account) *keppel.RegistryV2Error) (models.Account, *keppel.RegistryV2Error) {
	if account.Name == "" {
		return models.Account{}, keppel.AsRegistryV2Error(ErrAccountNameEmpty)
	}
	// reserve identifiers for internal pseudo-accounts and anything that might
	// appear like the first path element of a legal endpoint path on any of our
	// APIs (we will soon start recognizing image-like URLs such as
	// keppel.example.org/account/repo and offer redirection to a suitable UI;
	// this requires the account name to not overlap with API endpoint paths)
	if strings.HasPrefix(string(account.Name), "keppel") {
		return models.Account{}, keppel.AsRegistryV2Error(errors.New(`account names with the prefix "keppel" are reserved for internal use`)).WithStatus(http.StatusUnprocessableEntity)
	}
	if looksLikeAPIVersionRx.MatchString(string(account.Name)) {
		return models.Account{}, keppel.AsRegistryV2Error(errors.New(`account names that look like API versions (e.g. v1) are reserved for internal use`)).WithStatus(http.StatusUnprocessableEntity)
	}

	// check if account already exists
	originalAccount, err := keppel.FindAccount(p.db, account.Name)
	if err != nil {
		return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
	}
	if originalAccount != nil && originalAccount.AuthTenantID != account.AuthTenantID {
		return models.Account{}, keppel.AsRegistryV2Error(errors.New(`account name already in use by a different tenant`)).WithStatus(http.StatusConflict)
	}

	// PUT can either create a new account or update an existing account;
	// this distinction is important because several fields can only be set at creation
	var targetAccount models.Account
	if originalAccount == nil {
		targetAccount = models.Account{
			Name:                     account.Name,
			AuthTenantID:             account.AuthTenantID,
			SecurityScanPoliciesJSON: "[]",
			// all other attributes are set below or in the ApplyToAccount() methods called below
		}
	} else {
		targetAccount = *originalAccount
	}

	// validate and update fields as requested
	targetAccount.InMaintenance = account.InMaintenance

	// validate GC policies
	if len(account.GCPolicies) == 0 {
		targetAccount.GCPoliciesJSON = "[]"
	} else {
		for _, policy := range account.GCPolicies {
			err := policy.Validate()
			if err != nil {
				return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusUnprocessableEntity)
			}
		}
		buf, _ := json.Marshal(account.GCPolicies)
		targetAccount.GCPoliciesJSON = string(buf)
	}

	// serialize metadata
	if len(account.Metadata) == 0 {
		targetAccount.MetadataJSON = ""
	} else {
		buf, _ := json.Marshal(account.Metadata)
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
	if account.ReplicationPolicy == nil {
		if originalAccount == nil {
			replicationStrategy = keppel.NoReplicationStrategy
		} else {
			// PUT on existing account can omit replication policy to reuse existing policy
			replicationStrategy = originalStrategy
		}
	} else {
		// on existing accounts, we do not allow changing the strategy
		rp := *account.ReplicationPolicy
		if originalAccount != nil && originalStrategy != rp.Strategy {
			return models.Account{}, keppel.AsRegistryV2Error(keppel.ErrIncompatibleReplicationPolicy).WithStatus(http.StatusConflict)
		}

		err := rp.ApplyToAccount(&targetAccount)
		if errors.Is(err, keppel.ErrIncompatibleReplicationPolicy) {
			return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusConflict)
		} else if err != nil {
			return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusUnprocessableEntity)
		}
		replicationStrategy = rp.Strategy
	}

	// validate RBAC policies
	if len(account.RBACPolicies) == 0 {
		targetAccount.RBACPoliciesJSON = ""
	} else {
		for idx, policy := range account.RBACPolicies {
			err := policy.ValidateAndNormalize(replicationStrategy)
			if err != nil {
				return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusUnprocessableEntity)
			}
			account.RBACPolicies[idx] = policy
		}
		buf, _ := json.Marshal(account.RBACPolicies)
		targetAccount.RBACPoliciesJSON = string(buf)
	}

	// validate validation policy
	if account.ValidationPolicy != nil {
		rerr := account.ValidationPolicy.ApplyToAccount(&targetAccount)
		if rerr != nil {
			return models.Account{}, rerr
		}
	}

	var peer models.Peer
	if targetAccount.UpstreamPeerHostName != "" {
		// NOTE: This validates UpstreamPeerHostName as a side effect.
		peer, err = keppel.GetPeerFromAccount(p.db, targetAccount)
		if errors.Is(err, sql.ErrNoRows) {
			msg := fmt.Errorf(`unknown peer registry: %q`, targetAccount.UpstreamPeerHostName)
			return models.Account{}, keppel.AsRegistryV2Error(msg).WithStatus(http.StatusUnprocessableEntity)
		}
		if err != nil {
			return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
		}
	}

	// validate platform filter
	if originalAccount == nil {
		switch replicationStrategy {
		case keppel.NoReplicationStrategy:
			if account.PlatformFilter != nil {
				return models.Account{}, keppel.AsRegistryV2Error(errors.New(`platform filter is only allowed on replica accounts`)).WithStatus(http.StatusUnprocessableEntity)
			}
		case keppel.FromExternalOnFirstUseStrategy:
			targetAccount.PlatformFilter = account.PlatformFilter
		case keppel.OnFirstUseStrategy:
			// for internal replica accounts, the platform filter must match that of the primary account,
			// either by specifying the same filter explicitly or omitting it
			upstreamPlatformFilter, err := p.GetPlatformFilterFromPrimaryAccount(ctx, peer, targetAccount)
			if err != nil {
				return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
			}

			if account.PlatformFilter != nil && !upstreamPlatformFilter.IsEqualTo(account.PlatformFilter) {
				jsonPlatformFilter, _ := json.Marshal(account.PlatformFilter)
				jsonFilter, _ := json.Marshal(upstreamPlatformFilter)
				msg := fmt.Sprintf("peer account filter needs to match primary account filter: local account %s, peer account %s ", jsonPlatformFilter, jsonFilter)
				return models.Account{}, keppel.AsRegistryV2Error(errors.New(msg)).WithStatus(http.StatusConflict)
			}
			targetAccount.PlatformFilter = upstreamPlatformFilter
		}
	} else if account.PlatformFilter != nil && !originalAccount.PlatformFilter.IsEqualTo(account.PlatformFilter) {
		return models.Account{}, keppel.AsRegistryV2Error(errors.New(`cannot change platform filter on existing account`)).WithStatus(http.StatusConflict)
	}

	rerr := setCustomFields(&targetAccount)
	if rerr != nil {
		return models.Account{}, rerr
	}

	// create account if required
	if originalAccount == nil {
		// sublease tokens are only relevant when creating replica accounts
		subleaseTokenSecret := ""
		if targetAccount.UpstreamPeerHostName != "" {
			var rerr *keppel.RegistryV2Error
			subleaseTokenSecret, rerr = getSubleaseToken(peer)
			if rerr != nil {
				return models.Account{}, rerr.WithStatus(http.StatusBadRequest)
			}
		}

		// check permission to claim account name (this only happens here because
		// it's only relevant for account creations, not for updates)
		claimResult, err := p.fd.ClaimAccountName(ctx, targetAccount, subleaseTokenSecret)
		switch claimResult {
		case keppel.ClaimSucceeded:
			// nothing to do
		case keppel.ClaimFailed:
			// user error
			return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusForbidden)
		case keppel.ClaimErrored:
			// server error
			return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
		}

		err = p.sd.CanSetupAccount(ctx, targetAccount.Reduced())
		if err != nil {
			msg := fmt.Errorf("cannot set up backing storage for this account: %w", err)
			return models.Account{}, keppel.AsRegistryV2Error(msg).WithStatus(http.StatusConflict)
		}

		tx, err := p.db.Begin()
		if err != nil {
			return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
		}
		defer sqlext.RollbackUnlessCommitted(tx)

		err = tx.Insert(&targetAccount)
		if err != nil {
			return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
		}

		// commit the changes
		err = tx.Commit()
		if err != nil {
			return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
		}

		if userInfo != nil {
			p.auditor.Record(audittools.EventParameters{
				Time:       p.timeNow(),
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
			_, err := p.db.Update(&targetAccount)
			if err != nil {
				return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
			}
		}

		// audit log is necessary for all changes except to InMaintenance
		if userInfo != nil {
			originalAccount.InMaintenance = targetAccount.InMaintenance
			if !reflect.DeepEqual(*originalAccount, targetAccount) {
				p.auditor.Record(audittools.EventParameters{
					Time:       p.timeNow(),
					Request:    r,
					User:       userInfo,
					ReasonCode: http.StatusOK,
					Action:     cadf.UpdateAction,
					Target:     AuditAccount{Account: targetAccount},
				})
			}
		}
	}

	return targetAccount, nil
}

// DeleteAccountRemainingManifest appears in type DeleteAccountResponse.
type DeleteAccountRemainingManifest struct {
	RepositoryName string `json:"repository"`
	Digest         string `json:"digest"`
}

// DeleteAccountRemainingManifests appears in type DeleteAccountResponse.
type DeleteAccountRemainingManifests struct {
	Count uint64                           `json:"count"`
	Next  []DeleteAccountRemainingManifest `json:"next"`
}

// DeleteAccountRemainingBlobs appears in type DeleteAccountResponse.
type DeleteAccountRemainingBlobs struct {
	Count uint64 `json:"count"`
}

// DeleteAccountResponse is returned by Processor.DeleteAccount().
// It is the structure of the response to an account deletion API call.
type DeleteAccountResponse struct {
	RemainingManifests *DeleteAccountRemainingManifests `json:"remaining_manifests,omitempty"`
	RemainingBlobs     *DeleteAccountRemainingBlobs     `json:"remaining_blobs,omitempty"`
	Error              string                           `json:"error,omitempty"`
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

func (p *Processor) DeleteAccount(ctx context.Context, account models.Account, actx keppel.AuditContext) (*DeleteAccountResponse, error) {
	if !account.InMaintenance {
		return &DeleteAccountResponse{
			Error: "account must be set in maintenance first",
		}, nil
	}

	// can only delete account when user has deleted all manifests from it
	var nextManifests []DeleteAccountRemainingManifest
	err := sqlext.ForeachRow(p.db, deleteAccountFindManifestsQuery, []any{account.Name},
		func(rows *sql.Rows) error {
			var m DeleteAccountRemainingManifest
			err := rows.Scan(&m.RepositoryName, &m.Digest)
			nextManifests = append(nextManifests, m)
			return err
		},
	)
	if err != nil {
		return nil, err
	}
	if len(nextManifests) > 0 {
		manifestCount, err := p.db.SelectInt(deleteAccountCountManifestsQuery, account.Name)
		return &DeleteAccountResponse{
			RemainingManifests: &DeleteAccountRemainingManifests{
				Count: keppel.AtLeastZero(manifestCount),
				Next:  nextManifests,
			},
		}, err
	}

	// delete all repos (and therefore, all blob mounts), so that blob sweeping
	// can immediately take place
	_, err = p.db.Exec(deleteAccountReposQuery, account.Name)
	if err != nil {
		return nil, err
	}

	// can only delete account when all blobs have been deleted
	blobCount, err := p.db.SelectInt(deleteAccountCountBlobsQuery, account.Name)
	if err != nil {
		return nil, err
	}
	if blobCount > 0 {
		// make sure that blob sweep runs immediately
		_, err := p.db.Exec(deleteAccountMarkAllBlobsForDeletionQuery, account.Name, p.timeNow())
		if err != nil {
			return nil, err
		}
		_, err = p.db.Exec(deleteAccountScheduleBlobSweepQuery, account.Name, p.timeNow())
		if err != nil {
			return nil, err
		}
		return &DeleteAccountResponse{
			RemainingBlobs: &DeleteAccountRemainingBlobs{Count: keppel.AtLeastZero(blobCount)},
		}, nil
	}

	// start deleting the account in a transaction
	tx, err := p.db.Begin()
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
	err = p.sd.CleanupAccount(ctx, account.Reduced())
	if err != nil {
		return nil, fmt.Errorf("while cleaning up storage for account: %w", err)
	}
	err = p.fd.ForfeitAccountName(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("while cleaning up name claim for account: %w", err)
	}

	err = tx.Commit()
	if err != nil {
		return nil, err
	}

	if userInfo := actx.UserIdentity.UserInfo(); userInfo != nil {
		p.auditor.Record(audittools.EventParameters{
			Time:       p.timeNow(),
			Request:    actx.Request,
			User:       userInfo,
			ReasonCode: http.StatusOK,
			Action:     cadf.DeleteAction,
			Target:     AuditAccount{Account: account},
		})
	}

	return nil, nil
}
