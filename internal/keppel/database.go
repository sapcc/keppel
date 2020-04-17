/*******************************************************************************
*
* Copyright 2018 SAP SE
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

package keppel

import (
	"database/sql"
	"net/url"

	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/logg"
	gorp "gopkg.in/gorp.v2"
)

var sqlMigrations = map[string]string{
	"001_initial.up.sql": `
		CREATE TABLE accounts (
			name                   TEXT NOT NULL PRIMARY KEY,
			auth_tenant_id         TEXT NOT NULL,
			upstream_peer_hostname TEXT NOT NULL DEFAULT ''
		);

		CREATE TABLE rbac_policies (
			account_name     TEXT    NOT NULL REFERENCES accounts ON DELETE CASCADE,
			match_repository TEXT    NOT NULL,
			match_username   TEXT    NOT NULL,
			can_anon_pull    BOOLEAN NOT NULL DEFAULT FALSE,
			can_pull         BOOLEAN NOT NULL DEFAULT FALSE,
			can_push         BOOLEAN NOT NULL DEFAULT FALSE,
			can_delete       BOOLEAN NOT NULL DEFAULT FALSE,
			PRIMARY KEY (account_name, match_repository, match_username)
		);

		CREATE TABLE quotas (
			auth_tenant_id TEXT   NOT NULL PRIMARY KEY,
			manifests      BIGINT NOT NULL
		);

		CREATE TABLE peers (
			hostname                     TEXT        NOT NULL PRIMARY KEY,
			our_password                 TEXT        NOT NULL DEFAULT '',
			their_current_password_hash  TEXT        NOT NULL DEFAULT '',
			their_previous_password_hash TEXT        NOT NULL DEFAULT '',
			last_peered_at               TIMESTAMPTZ DEFAULT NULL
		);

		CREATE TABLE repos (
			id           BIGSERIAL NOT NULL PRIMARY KEY,
			account_name TEXT      NOT NULL REFERENCES accounts ON DELETE CASCADE,
			name         TEXT      NOT NULL
		);

		CREATE TABLE blobs (
			id           BIGSERIAL   NOT NULL PRIMARY KEY,
			account_name TEXT        NOT NULL REFERENCES accounts ON DELETE CASCADE,
			digest       TEXT        NOT NULL,
			size_bytes   BIGINT      NOT NULL,
			storage_id   TEXT        NOT NULL,
			pushed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (account_name, digest)
		);

		CREATE TABLE blob_mounts (
			blob_id BIGINT NOT NULL REFERENCES blobs ON DELETE CASCADE,
			repo_id BIGINT NOT NULL REFERENCES repos ON DELETE CASCADE,
			UNIQUE (blob_id, repo_id)
		);

		CREATE TABLE uploads (
			repo_id     BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
			uuid        TEXT        NOT NULL,
			storage_id  TEXT        NOT NULL,
			size_bytes  BIGINT      NOT NULL,
			digest      TEXT        NOT NULL,
			num_chunks  INT         NOT NULL,
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (repo_id, uuid)
		);

		CREATE TABLE manifests (
			repo_id    BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
			digest     TEXT        NOT NULL,
			media_type TEXT        NOT NULL,
			size_bytes BIGINT      NOT NULL,
			pushed_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (repo_id, digest)
		);

		CREATE TABLE tags (
			repo_id    BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
			name       TEXT        NOT NULL,
			digest     TEXT        NOT NULL,
			pushed_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (repo_id, name),
			FOREIGN KEY (repo_id, digest) REFERENCES manifests ON DELETE CASCADE
		);

		CREATE TABLE pending_blobs (
			repo_id BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
			digest  TEXT        NOT NULL,
			reason  TEXT        NOT NULL,
			since   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (repo_id, digest)
		);
	`,
	"001_initial.down.sql": `
		DROP TABLE pending_blobs;
		DROP TABLE tags;
		DROP TABLE manifests;
		DROP TABLE uploads;
		DROP TABLE blob_mounts;
		DROP TABLE blobs;
		DROP TABLE repos;
		DROP TABLE peers;
		DROP TABLE quotas;
		DROP TABLE rbac_policies;
		DROP TABLE accounts;
	`,
	"002_add_account_required_labels.up.sql": `
		ALTER TABLE accounts ADD column required_labels TEXT NOT NULL DEFAULT '';
	`,
	"002_add_account_required_labels.down.sql": `
		ALTER TABLE accounts DROP column required_labels;
	`,
	"003_add_repos_uniqueness_constraint.up.sql": `
		ALTER TABLE repos ADD CONSTRAINT repos_account_name_name_key UNIQUE (account_name, name);
	`,
	"003_add_repos_uniqueness_constraint.down.sql": `
		ALTER TABLE repos DROP CONSTRAINT repos_account_name_name_key;
	`,
	"004_add_manifest_subreferences.up.sql": `
		CREATE TABLE manifest_blob_refs (
			repo_id BIGINT NOT NULL,
			digest  TEXT   NOT NULL,
			blob_id BIGINT NOT NULL       REFERENCES blobs ON DELETE RESTRICT,
			FOREIGN KEY (repo_id, digest) REFERENCES manifests ON DELETE CASCADE,
			UNIQUE (repo_id, digest, blob_id)
		);
		CREATE TABLE manifest_manifest_refs (
			repo_id       BIGINT NOT NULL,
			parent_digest TEXT   NOT NULL,
			child_digest  TEXT   NOT NULL,
			FOREIGN KEY (repo_id, parent_digest) REFERENCES manifests (repo_id, digest) ON DELETE CASCADE,
			FOREIGN KEY (repo_id, child_digest)  REFERENCES manifests (repo_id, digest) ON DELETE RESTRICT,
			UNIQUE (repo_id, parent_digest, child_digest)
		);
		ALTER TABLE manifests
			ADD COLUMN validated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			ADD COLUMN validation_error_message TEXT        NOT NULL DEFAULT '';
		ALTER TABLE blobs
			ADD COLUMN validated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			ADD COLUMN validation_error_message TEXT        NOT NULL DEFAULT '';
	`,
	"004_add_manifest_subreferences.down.sql": `
		DROP TABLE manifest_blob_refs;
		DROP TABLE manifest_manifest_refs;
		ALTER TABLE manifests
			DROP COLUMN validated_at,
			DROP COLUMN validation_error_message;
		ALTER TABLE blobs
			DROP COLUMN validated_at,
			DROP COLUMN validation_error_message;
	`,
	"005_rebase_pending_blobs_on_accounts.up.sql": `
		DROP TABLE pending_blobs;
		CREATE TABLE pending_blobs (
			account_name TEXT        NOT NULL REFERENCES accounts ON DELETE CASCADE,
			digest       TEXT        NOT NULL,
			reason       TEXT        NOT NULL,
			since        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (account_name, digest)
		);
	`,
	"005_rebase_pending_blobs_on_accounts.down.sql": `
		DROP TABLE pending_blobs;
		CREATE TABLE pending_blobs (
			repo_id BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
			digest  TEXT        NOT NULL,
			reason  TEXT        NOT NULL,
			since   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (repo_id, digest)
		);
	`,
	"006_forbid_blob_mount_deletion_while_referenced.up.sql": `
		ALTER TABLE manifest_blob_refs
			DROP CONSTRAINT manifest_blob_refs_blob_id_fkey,
			ADD FOREIGN KEY (blob_id, repo_id) REFERENCES blob_mounts (blob_id, repo_id) ON DELETE RESTRICT;
	`,
	"006_forbid_blob_mount_deletion_while_referenced.down.sql": `
		ALTER TABLE manifest_blob_refs
			DROP CONSTRAINT manifest_blob_refs_blob_id_repo_id_fkey,
			ADD FOREIGN KEY blob_id REFERENCES blobs (id) ON DELETE RESTRICT;
	`,
	"007_add_garbage_collection_timestamps.up.sql": `
		ALTER TABLE accounts
			ADD COLUMN blobs_sweeped_at   TIMESTAMPTZ DEFAULT NULL,
			ADD COLUMN storage_sweeped_at TIMESTAMPTZ DEFAULT NULL;
		ALTER TABLE repos       ADD COLUMN blob_mounts_sweeped_at TIMESTAMPTZ DEFAULT NULL;
		ALTER TABLE blobs       ADD COLUMN marked_for_deletion_at TIMESTAMPTZ DEFAULT NULL;
		ALTER TABLE blob_mounts ADD COLUMN marked_for_deletion_at TIMESTAMPTZ DEFAULT NULL;
	`,
	"007_add_garbage_collection_timestamps.down.sql": `
		ALTER TABLE accounts
			DROP COLUMN blobs_sweeped_at,
			DROP COLUMN storage_sweeped_at;
		ALTER TABLE repos       DROP COLUMN blob_mounts_sweeped_at;
		ALTER TABLE blobs       DROP COLUMN marked_for_deletion_at;
		ALTER TABLE blob_mounts DROP COLUMN marked_for_deletion_at;
	`,
	"008_add_repos_manifests_synced_at.up.sql": `
		ALTER TABLE repos ADD COLUMN manifests_synced_at TIMESTAMPTZ DEFAULT NULL;
	`,
	"008_add_repos_manifests_synced_at.down.sql": `
		ALTER TABLE repos DROP COLUMN manifests_synced_at;
	`,
	"009_add_tables_for_storage_sweep.up.sql": `
		CREATE TABLE unknown_blobs (
			account_name           TEXT        NOT NULL REFERENCES accounts ON DELETE CASCADE,
			storage_id             TEXT        NOT NULL,
			marked_for_deletion_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (account_name, storage_id)
		);
		CREATE TABLE unknown_manifests (
			account_name           TEXT        NOT NULL REFERENCES accounts ON DELETE CASCADE,
			repo_name              TEXT        NOT NULL,
			digest                 TEXT        NOT NULL,
			marked_for_deletion_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (account_name, repo_name, digest)
		);
	`,
	"009_add_tables_for_storage_sweep.down.sql": `
		DROP TABLE unknown_blobs;
		DROP TABLE unknown_manifests;
	`,
	"010_add_account_metadata.up.sql": `
		ALTER TABLE accounts ADD COLUMN metadata_json TEXT NOT NULL DEFAULT '';
	`,
	"010_add_account_metadata.down.sql": `
		ALTER TABLE accounts DROP COLUMN metadata_json;
	`,
	"011_add_account_announced_to_federation_at.up.sql": `
		ALTER TABLE accounts ADD COLUMN announced_to_federation_at TIMESTAMPTZ DEFAULT NULL;
	`,
	"011_add_account_announced_to_federation_at.down.sql": `
		ALTER TABLE accounts DROP COLUMN announced_to_federation_at;
	`,
}

//DB adds convenience functions on top of gorp.DbMap.
type DB struct {
	gorp.DbMap
}

//InitDB connects to the Postgres database.
func InitDB(dbURL url.URL) (*DB, error) {
	db, err := easypg.Connect(easypg.Configuration{
		PostgresURL: &dbURL,
		Migrations:  sqlMigrations,
	})
	if err != nil {
		return nil, err
	}

	result := &DB{DbMap: gorp.DbMap{Db: db, Dialect: gorp.PostgresDialect{}}}
	initModels(&result.DbMap)
	return result, nil
}

//RollbackUnlessCommitted calls Rollback() on a transaction if it hasn't been
//committed or rolled back yet. Use this with the defer keyword to make sure
//that a transaction is automatically rolled back when a function fails.
func RollbackUnlessCommitted(tx *gorp.Transaction) {
	err := tx.Rollback()
	switch err {
	case nil:
		//rolled back successfully
		logg.Info("implicit rollback done")
		return
	case sql.ErrTxDone:
		//already committed or rolled back - nothing to do
		return
	default:
		logg.Error("implicit rollback failed: %s", err.Error())
	}
}

//ForeachRow calls dbi.Query() with the given query and args, then executes the
//given action one for every row in the result set. It then cleans up the
//result set, and it handles any errors that occur during all of this.
func ForeachRow(dbi gorp.SqlExecutor, query string, args []interface{}, action func(*sql.Rows) error) error {
	rows, err := dbi.Query(query, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		err = action(rows)
		if err != nil {
			rows.Close()
			return err
		}
	}
	err = rows.Err()
	if err != nil {
		rows.Close()
		return err
	}
	return rows.Close()
}

//StmtPreparer is anything that has the classical Prepare() method like *sql.DB
//or *sql.Tx.
type StmtPreparer interface {
	Prepare(query string) (*sql.Stmt, error)
}

//WithPreparedStatement calls dbi.Prepare() and passes the resulting prepared statement
//into the given action. It then cleans up the prepared statements, and it
//handles any errors that occur during all of this.
func WithPreparedStatement(dbi StmtPreparer, query string, action func(*sql.Stmt) error) error {
	stmt, err := dbi.Prepare(query)
	if err != nil {
		return err
	}
	err = action(stmt)
	if err != nil {
		stmt.Close()
		return err
	}
	return stmt.Close()
}
