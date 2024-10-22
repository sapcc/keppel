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

package tasks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/jobloop"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/processor"
)

// EnforceManagedAccounts is a job. Each task creates newly discovered accounts from the driver.
func (j *Janitor) DeleteAccountsJob(registerer prometheus.Registerer) jobloop.Job {
	return (&jobloop.ProducerConsumerJob[models.AccountName]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "delete accounts marked for deletion",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_account_deletion",
				Help: "Counter for deleted accounts.",
			},
		},
		DiscoverTask: j.discoverAccountForDeletion,
		ProcessTask:  j.deleteMarkedAccount,
	}).Setup(registerer)
}

var (
	accountDeletionSelectQuery = sqlext.SimplifyWhitespace(`
		SELECT name FROM accounts
		WHERE is_deleting AND next_deletion_attempt_at < $1
		ORDER BY next_deletion_attempt_at ASC, name ASC
	`)
)

func (j *Janitor) discoverAccountForDeletion(_ context.Context, _ prometheus.Labels) (accountName models.AccountName, err error) {
	err = j.db.SelectOne(&accountName, accountDeletionSelectQuery, j.timeNow())
	return accountName, err
}

func (j *Janitor) deleteMarkedAccount(ctx context.Context, accountName models.AccountName, labels prometheus.Labels) error {
	accountModel, err := keppel.FindAccount(j.db, accountName)
	if errors.Is(err, sql.ErrNoRows) {
		// assume the account got already deleted
		return nil
	}
	if err != nil {
		return err
	}

	actx := keppel.AuditContext{
		UserIdentity: janitorUserIdentity{TaskName: "account-deletion"},
		Request:      janitorDummyRequest,
	}

	// can only delete account when all manifests from it are deleted
	err = sqlext.ForeachRow(j.db, deleteAccountFindManifestsQuery, []any{accountModel.Name},
		func(rows *sql.Rows) error {
			var m deleteAccountRemainingManifest
			err := rows.Scan(&m.RepositoryName, &m.Digest)
			if err != nil {
				return err
			}

			parsedDigest, err := digest.Parse(m.Digest)
			if err != nil {
				return fmt.Errorf("while deleting manifest %q in repository %q: could not parse digest: %w",
					m.Digest, m.RepositoryName, err)
			}
			repo, err := keppel.FindRepository(j.db, m.RepositoryName, accountModel.Name)
			if err != nil {
				return fmt.Errorf("while deleting manifest %q in repository %q: could not find repository in DB: %w",
					m.Digest, m.RepositoryName, err)
			}
			err = j.processor().DeleteManifest(ctx, accountModel.Reduced(), *repo, parsedDigest, actx)
			if err != nil {
				return fmt.Errorf("while deleting manifest %q in repository %q: %w",
					m.Digest, m.RepositoryName, err)
			}

			return nil
		},
	)
	if err != nil {
		return err
	}

	// delete all repos (and therefore, all blob mounts), so that blob sweeping can immediately take place
	_, err = j.db.Exec(deleteAccountReposQuery, accountModel.Name)
	if err != nil {
		return err
	}

	// can only delete account when all blobs have been deleted
	blobCount, err := j.db.SelectInt(deleteAccountCountBlobsQuery, accountModel.Name)
	if err != nil {
		return err
	}
	if blobCount > 0 {
		// make sure that blob sweep runs immediately
		// TODO: how to prevent resetting time stamp if already set?
		_, err := j.db.Exec(deleteAccountMarkAllBlobsForDeletionQuery, accountModel.Name, j.timeNow())
		if err != nil {
			return err
		}

		_, err = j.db.Exec(deleteAccountScheduleBlobSweepQuery, accountModel.Name, j.timeNow())
		if err != nil {
			return err
		}

		_, err = j.db.Exec(`UPDATE accounts SET next_deletion_attempt_at = $1 WHERE name = $2`, j.timeNow().Add(1*time.Minute), accountModel.Name)
		if err != nil {
			return err
		}
		logg.Info("cleaning up managed account %q: waiting for %d blobs to be deleted", accountModel.Name, blobCount)
		return nil
	}

	// start deleting the account in a transaction
	tx, err := j.db.Begin()
	if err != nil {
		return err
	}
	defer sqlext.RollbackUnlessCommitted(tx)
	_, err = tx.Delete(accountModel)
	if err != nil {
		return err
	}

	// before committing the transaction, confirm account deletion with the
	// storage driver and the federation driver
	err = j.sd.CleanupAccount(ctx, accountModel.Reduced())
	if err != nil {
		return fmt.Errorf("while cleaning up storage for account: %w", err)
	}
	err = j.fd.ForfeitAccountName(ctx, *accountModel)
	if err != nil {
		return fmt.Errorf("while cleaning up name claim for account: %w", err)
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	if userInfo := actx.UserIdentity.UserInfo(); userInfo != nil {
		j.auditor.Record(audittools.EventParameters{
			Time:       j.timeNow(),
			Request:    actx.Request,
			User:       userInfo,
			ReasonCode: http.StatusOK,
			Action:     cadf.DeleteAction,
			Target:     processor.AuditAccount{Account: *accountModel},
		})
	}

	return nil
}

var (
	deleteAccountFindManifestsQuery = sqlext.SimplifyWhitespace(`
		SELECT r.name, m.digest
			FROM manifests m
			JOIN repos r ON m.repo_id = r.id
			JOIN accounts a ON a.name = r.account_name
			LEFT OUTER JOIN manifest_manifest_refs mmr ON mmr.repo_id = r.id AND m.digest = mmr.child_digest
    WHERE a.name = $1 AND parent_digest IS NULL
	`)
	deleteAccountReposQuery                   = `DELETE FROM repos WHERE account_name = $1`
	deleteAccountCountBlobsQuery              = `SELECT COUNT(id) FROM blobs WHERE account_name = $1`
	deleteAccountScheduleBlobSweepQuery       = `UPDATE accounts SET next_blob_sweep_at = $2 WHERE name = $1`
	deleteAccountMarkAllBlobsForDeletionQuery = `UPDATE blobs SET can_be_deleted_at = $2 WHERE account_name = $1`
)

type deleteAccountRemainingManifest struct {
	RepositoryName string
	Digest         string
}
