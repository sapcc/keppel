/*******************************************************************************
*
* Copyright 2018-2020 SAP SE
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
	"context"
	"database/sql"
	"net/url"

	"github.com/go-gorp/gorp/v3"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/logg"
)

var sqlMigrations = map[string]string{
	//NOTE: Migrations 1 through 31 have been rolled up into one at 2023-03-14
	//to better represent the current baseline of the DB schema.
	"031_rollup.up.sql": `
		CREATE TABLE accounts (
			name                            TEXT        NOT NULL PRIMARY KEY,
			auth_tenant_id                  TEXT        NOT NULL,
			upstream_peer_hostname          TEXT        NOT NULL DEFAULT '',
			required_labels                 TEXT        NOT NULL DEFAULT '',
			metadata_json                   TEXT        NOT NULL DEFAULT '',
			next_blob_sweep_at              TIMESTAMPTZ DEFAULT NULL,
			next_storage_sweep_at           TIMESTAMPTZ DEFAULT NULL,
			next_federation_announcement_at TIMESTAMPTZ DEFAULT NULL,
			in_maintenance                  BOOLEAN     NOT NULL DEFAULT FALSE,
			external_peer_url               TEXT        NOT NULL DEFAULT '',
			external_peer_username          TEXT        NOT NULL DEFAULT '',
			external_peer_password          TEXT        NOT NULL DEFAULT '',
			platform_filter                 TEXT        NOT NULL DEFAULT '',
			gc_policies_json                TEXT        NOT NULL DEFAULT '[]'
		);

		CREATE TABLE rbac_policies (
			account_name        TEXT    NOT NULL REFERENCES accounts ON DELETE CASCADE,
			match_repository    TEXT    NOT NULL,
			match_username      TEXT    NOT NULL,
			can_anon_pull       BOOLEAN NOT NULL DEFAULT FALSE,
			can_pull            BOOLEAN NOT NULL DEFAULT FALSE,
			can_push            BOOLEAN NOT NULL DEFAULT FALSE,
			can_delete          BOOLEAN NOT NULL DEFAULT FALSE,
			match_cidr          TEXT    NOT NULL DEFAULT '0.0.0.0/0',
			can_anon_first_pull BOOLEAN NOT NULL DEFAULT FALSE,
			PRIMARY KEY (account_name, match_cidr, match_repository, match_username)
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
			id                       BIGSERIAL   NOT NULL PRIMARY KEY,
			account_name             TEXT        NOT NULL REFERENCES accounts ON DELETE CASCADE,
			name                     TEXT        NOT NULL,
			next_blob_mount_sweep_at TIMESTAMPTZ DEFAULT NULL,
			next_manifest_sync_at    TIMESTAMPTZ DEFAULT NULL,
			next_gc_at               TIMESTAMPTZ DEFAULT NULL,
			UNIQUE (account_name, name)
		);

		CREATE TABLE blobs (
			id                       BIGSERIAL   NOT NULL PRIMARY KEY,
			account_name             TEXT        NOT NULL REFERENCES accounts ON DELETE CASCADE,
			digest                   TEXT        NOT NULL,
			size_bytes               BIGINT      NOT NULL,
			storage_id               TEXT        NOT NULL,
			pushed_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			validated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			validation_error_message TEXT        NOT NULL DEFAULT '',
			can_be_deleted_at        TIMESTAMPTZ DEFAULT NULL,
			media_type               TEXT        NOT NULL DEFAULT '',
			blocks_vuln_scanning     BOOLEAN     DEFAULT NULL,
			UNIQUE (account_name, digest)
		);

		CREATE TABLE blob_mounts (
			blob_id                BIGINT      NOT NULL REFERENCES blobs ON DELETE CASCADE,
			repo_id                BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
			can_be_deleted_at      TIMESTAMPTZ DEFAULT NULL,
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
			repo_id                  BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
			digest                   TEXT        NOT NULL,
			media_type               TEXT        NOT NULL,
			size_bytes               BIGINT      NOT NULL,
			pushed_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			validated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			validation_error_message TEXT        NOT NULL DEFAULT '',
			last_pulled_at           TIMESTAMPTZ DEFAULT NULL,
			labels_json              TEXT        NOT NULL DEFAULT '',
			gc_status_json           TEXT        NOT NULL DEFAULT '',
			min_layer_created_at     TIMESTAMPTZ DEFAULT NULL,
			max_layer_created_at     TIMESTAMPTZ DEFAULT NULL,
			PRIMARY KEY (repo_id, digest)
		);

		CREATE TABLE manifest_contents (
			repo_id BIGINT NOT NULL,
			digest  TEXT   NOT NULL,
			content BYTEA  NOT NULL,
			FOREIGN KEY (repo_id, digest) REFERENCES manifests ON DELETE CASCADE,
			UNIQUE (repo_id, digest)
		);

		CREATE TABLE manifest_blob_refs (
			repo_id BIGINT NOT NULL,
			digest  TEXT   NOT NULL,
			blob_id BIGINT NOT NULL,
			FOREIGN KEY (repo_id, digest) REFERENCES manifests ON DELETE CASCADE,
			FOREIGN KEY (blob_id, repo_id) REFERENCES blob_mounts (blob_id, repo_id) ON DELETE RESTRICT,
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

		CREATE TABLE tags (
			repo_id        BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
			name           TEXT        NOT NULL,
			digest         TEXT        NOT NULL,
			pushed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_pulled_at TIMESTAMPTZ DEFAULT NULL,
			PRIMARY KEY (repo_id, name),
			FOREIGN KEY (repo_id, digest) REFERENCES manifests ON DELETE CASCADE
		);

		CREATE TABLE vuln_info (
			repo_id             BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
			digest              TEXT        NOT NULL,
			status              TEXT        NOT NULL,
			message             TEXT        NOT NULL,
			next_check_at       TIMESTAMPTZ NOT NULL,
			checked_at          TIMESTAMPTZ DEFAULT NULL,        -- NULL before first check
			index_started_at    TIMESTAMPTZ DEFAULT NULL,        -- NULL if not submitted to Clair yet
			index_finished_at   TIMESTAMPTZ DEFAULT NULL,        -- NULL until index report is ready
			index_state         TEXT        NOT NULL DEFAULT '',
			check_duration_secs REAL        DEFAULT NULL,        -- NULL before first check
			FOREIGN KEY (repo_id, digest) REFERENCES manifests ON DELETE CASCADE,
			UNIQUE (repo_id, digest)
		);

		CREATE TABLE pending_blobs (
			account_name TEXT        NOT NULL REFERENCES accounts ON DELETE CASCADE,
			digest       TEXT        NOT NULL,
			reason       TEXT        NOT NULL,
			since        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (account_name, digest)
		);

		CREATE TABLE unknown_blobs (
			account_name      TEXT        NOT NULL REFERENCES accounts ON DELETE CASCADE,
			storage_id        TEXT        NOT NULL,
			can_be_deleted_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (account_name, storage_id)
		);

		CREATE TABLE unknown_manifests (
			account_name      TEXT        NOT NULL REFERENCES accounts ON DELETE CASCADE,
			repo_name         TEXT        NOT NULL,
			digest            TEXT        NOT NULL,
			can_be_deleted_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (account_name, repo_name, digest)
		);
	`,
	"031_rollup.down.sql": `
		DROP TABLE unknown_manifests;
		DROP table unknown_blobs;
		DROP TABLE pending_blobs;
		DROP TABLE vuln_info;
		DROP TABLE tags;
		DROP TABLE manifest_manifest_refs;
		DROP TABLE manifest_blob_refs;
		DROP TABLE manifest_contents;
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
	"032_trivy.up.sql": `
		CREATE TABLE trivy_security_info (
			repo_id             BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
			digest              TEXT        NOT NULL,
			status              TEXT        NOT NULL,
			message             TEXT        NOT NULL,
			next_check_at       TIMESTAMPTZ NOT NULL,
			checked_at          TIMESTAMPTZ DEFAULT NULL,        -- NULL before first check
			check_duration_secs REAL        DEFAULT NULL,        -- NULL before first check
			FOREIGN KEY (repo_id, digest) REFERENCES manifests ON DELETE CASCADE,
			UNIQUE (repo_id, digest)
		);

		INSERT INTO trivy_security_info(repo_id, digest, status, message, next_check_at)
			select repo_id, digest, 'Pending', '', NOW() from manifests;
	`,
	"032_trivy.down.sql": `
		DROP TABLE trivy_security_info;
	`,
	"033_trivy.up.sql": `
		ALTER TABLE trivy_security_info RENAME COLUMN status TO vuln_status;
	`,
	"033_trivy.down.sql": `
		ALTER TABLE trivy_security_info RENAME COLUMN vuln_status TO status;
	`,
	"034_security_scan_policies.up.sql": `
		ALTER TABLE accounts ADD COLUMN security_scan_policies_json TEXT NOT NULL DEFAULT '[]';
	`,
	"034_security_scan_policies.down.sql": `
		ALTER TABLE accounts DROP COLUMN security_scan_policies_json;
	`,
	"035_bye_bye_clair.up.sql": `
		DROP TABLE vuln_info;
	`,
	"035_bye_bye_clair.down.sql": `
		CREATE TABLE vuln_info (
			repo_id             BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
			digest              TEXT        NOT NULL,
			status              TEXT        NOT NULL,
			message             TEXT        NOT NULL,
			next_check_at       TIMESTAMPTZ NOT NULL,
			checked_at          TIMESTAMPTZ DEFAULT NULL,        -- NULL before first check
			index_started_at    TIMESTAMPTZ DEFAULT NULL,        -- NULL if not submitted to Clair yet
			index_finished_at   TIMESTAMPTZ DEFAULT NULL,        -- NULL until index report is ready
			index_state         TEXT        NOT NULL DEFAULT '',
			check_duration_secs REAL        DEFAULT NULL,        -- NULL before first check
			FOREIGN KEY (repo_id, digest) REFERENCES manifests ON DELETE CASCADE,
			UNIQUE (repo_id, digest)
		);
	`,
}

// DB adds convenience functions on top of gorp.DbMap and reimplements the SqlExecutor interface to enforce context'ed function calls
type DB struct {
	gorp.DbMap
}

// Deprecated: use db.withContext!
func (db *DB) Get(i interface{}, keys ...interface{}) (interface{}, error) {
	return db.WithContext(context.TODO()).Get(i, keys)
}

// Deprecated: use db.withContext!
func (db *DB) Insert(list ...interface{}) error {
	return db.WithContext(context.TODO()).Insert(list...)
}

// Deprecated: use db.withContext!
func (db *DB) Update(list ...interface{}) (int64, error) {
	return db.WithContext(context.TODO()).Update(list...)
}

// Deprecated: use db.withContext!
func (db *DB) Delete(list ...interface{}) (int64, error) {
	return db.WithContext(context.TODO()).Delete(list...)
}

// Deprecated: use db.withContext!
func (db *DB) Exec(query string, args ...interface{}) (sql.Result, error) {
	return db.WithContext(context.TODO()).Exec(query, args...)
}

// Deprecated: use db.withContext!
func (db *DB) Select(i interface{}, query string, args ...interface{}) ([]interface{}, error) {
	return db.WithContext(context.TODO()).Select(i, query, args...)
}

// Deprecated: use db.withContext!
func (db *DB) SelectInt(query string, args ...interface{}) (int64, error) {
	return db.WithContext(context.TODO()).SelectInt(query, args...)
}

// Deprecated: use db.withContext!
func (db *DB) SelectNullInt(query string, args ...interface{}) (sql.NullInt64, error) {
	return db.WithContext(context.TODO()).SelectNullInt(query, args...)
}

// Deprecated: use db.withContext!
func (db *DB) SelectFloat(query string, args ...interface{}) (float64, error) {
	return db.WithContext(context.TODO()).SelectFloat(query, args...)
}

// Deprecated: use db.withContext!
func (db *DB) SelectNullFloat(query string, args ...interface{}) (sql.NullFloat64, error) {
	return db.WithContext(context.TODO()).SelectNullFloat(query, args...)
}

// Deprecated: use db.withContext!
func (db *DB) SelectStr(query string, args ...interface{}) (string, error) {
	return db.WithContext(context.TODO()).SelectStr(query, args...)
}

// Deprecated: use db.withContext!
func (db *DB) SelectNullStr(query string, args ...interface{}) (sql.NullString, error) {
	return db.WithContext(context.TODO()).SelectNullStr(query, args...)
}

// Deprecated: use db.withContext!
func (db *DB) SelectOne(holder interface{}, query string, args ...interface{}) error {
	return db.WithContext(context.TODO()).SelectOne(holder, query, args...)
}

// Deprecated: use db.withContext!
func (db *DB) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return db.WithContext(context.TODO()).Query(query, args...)
}

// Deprecated: use db.withContext!
func (db *DB) QueryRow(query string, args ...interface{}) *sql.Row {
	return db.WithContext(context.TODO()).QueryRow(query, args...)
}

// convience functions

// SelectBool is analogous to the other SelectFoo() functions from gorp.DbMap
// like SelectFloat, SelectInt, SelectStr, etc.
func (db *DB) SelectBool(query string, args ...interface{}) (bool, error) {
	var result bool
	err := db.WithContext(context.TODO()).QueryRow(query, args...).Scan(&result)
	return result, err
}

// InitDB connects to the Postgres database.
func InitDB(dbURL *url.URL) (*DB, error) {
	logg.Debug("initializing DB connection...")

	db, err := easypg.Connect(easypg.Configuration{
		PostgresURL: dbURL,
		Migrations:  sqlMigrations,
	})
	if err != nil {
		return nil, err
	}
	//ensure that this process does not starve other Keppel processes for DB connections
	db.SetMaxOpenConns(16)

	result := &DB{DbMap: gorp.DbMap{Db: db, Dialect: gorp.PostgresDialect{}}}
	initModels(&result.DbMap)
	return result, nil
}
