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
	"database/sql"

	"github.com/go-gorp/gorp/v3"
	"github.com/sapcc/go-bits/easypg"

	"github.com/sapcc/keppel/internal/models"
)

var sqlMigrations = map[string]string{
	//NOTE: Migrations 1 through 35 have been rolled up into one at 2024-02-26
	// to better represent the current baseline of the DB schema.
	"035_rollup.up.sql": `
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
			gc_policies_json                TEXT        NOT NULL DEFAULT '[]',
			security_scan_policies_json     TEXT        NOT NULL DEFAULT '[]'
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

		CREATE TABLE trivy_security_info (
			repo_id             BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
			digest              TEXT        NOT NULL,
			vuln_status         TEXT        NOT NULL,
			message             TEXT        NOT NULL,
			next_check_at       TIMESTAMPTZ NOT NULL,
			checked_at          TIMESTAMPTZ DEFAULT NULL,        -- NULL before first check
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
	"035_rollup.down.sql": `
		DROP TABLE unknown_manifests;
		DROP table unknown_blobs;
		DROP TABLE pending_blobs;
		DROP TABLE trivy_security_info;
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
	"036_add_accounts_rbac_policies_json.up.sql": `
		ALTER TABLE accounts
			ADD COLUMN rbac_policies_json TEXT NOT NULL DEFAULT '';
	`,
	"036_add_accounts_rbac_policies_json.down.sql": `
		ALTER TABLE accounts
			DROP COLUMN rbac_policies_json;
	`,
	"037_drop_rbac_policies_table.up.sql": `
		DROP TABLE rbac_policies;
	`,
	"037_drop_rbac_policies_table.down.sql": `
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
	`,
	"038_convert_validated_at_to_next_validation_at.up.sql": `
		ALTER TABLE blobs
			DROP COLUMN validated_at,
			ADD COLUMN next_validation_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
		ALTER TABLE manifests
			DROP COLUMN validated_at,
			ADD COLUMN next_validation_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
	`,
	"038_convert_validated_at_to_next_validation_at.down.sql": `
		ALTER TABLE blobs
			DROP COLUMN next_validation_at,
			ADD COLUMN validated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
		ALTER TABLE manifests
			DROP COLUMN next_validation_at,
			ADD COLUMN validated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
	`,
	// Re 039: These indices are used when selecting tasks for BlobValidationJob
	// and ManifestValidationJob. Before we added indices here, those queries
	// were consistently the most expensive by total execution time.
	"039_add_indices_on_next_validation_at.up.sql": `
		CREATE INDEX ON blobs (next_validation_at);
		CREATE INDEX ON manifests (next_validation_at);
	`,
	"039_add_indices_on_next_validation_at.down.sql": `
		DROP INDEX blobs_next_validation_at_idx;
		DROP INDEX manifests_next_validation_at_idx;
	`,
	// Re 040: index is used by BlobMountSweepJob
	"040_add_index_blob_mounts.up.sql": `
		CREATE INDEX ON blob_mounts (can_be_deleted_at NULLS FIRST, repo_id);
		CREATE INDEX ON manifests (validation_error_message) WHERE validation_error_message != '';
	`,
	"040_add_index_blob_mounts.down.sql": `
		DROP INDEX blob_mounts_can_be_deleted_at_repo_id_idx;
		DROP INDEX manifests_validation_error_message_idx;
	`,
	"041_add_accounts_is_managed.up.sql": `
		ALTER TABLE accounts
			ADD COLUMN is_managed BOOLEAN NOT NULL DEFAULT FALSE,
			ADD COLUMN next_enforcement_at TIMESTAMPTZ DEFAULT NULL;
	`,
	"041_add_accounts_is_managed.down.sql": `
		ALTER TABLE accounts
			DROP COLUMN is_managed, next_enforcement_at;
	`,
	"042_add_peers_use_for_pull_delegation.up.sql": `
		ALTER TABLE peers
			ADD COLUMN use_for_pull_delegation BOOLEAN NOT NULL DEFAULT TRUE;
	`,
	"042_add_peers_use_for_pull_delegation.down.sql": `
		ALTER TABLE peers
			DROP COLUMN use_for_pull_delegation;
	`,
	"043_add_accounts_is_deleting.up.sql": `
		ALTER TABLE accounts
			ADD COLUMN is_deleting BOOLEAN NOT NULL DEFAULT FALSE,
			ADD COLUMN next_deletion_attempt_at TIMESTAMPTZ DEFAULT NULL;

		UPDATE accounts SET is_deleting = TRUE WHERE in_maintenance;

		ALTER TABLE accounts
			DROP COLUMN in_maintenance,
			DROP COLUMN metadata_json;
	`,
	"043_add_accounts_is_deleting.down.sql": `
		ALTER TABLE accounts
			ADD COLUMN in_maintenance BOOLEAN NOT NULL DEFAULT FALSE,
			ADD COLUMN metadata_json TEXT NOT NULL DEFAULT '';

		UPDATE accounts SET in_maintenance = TRUE WHERE is_deleting;

		ALTER TABLE accounts
			DROP COLUMN is_deleting,
			DROP COLUMN next_deletion_attempt_at;
	`,
	"044_bring_back_accounts_in_maintenance_as_dummy.up.sql": `
		ALTER TABLE accounts
			ADD COLUMN in_maintenance BOOLEAN NOT NULL DEFAULT FALSE;
	`,
	"044_bring_back_accounts_in_maintenance_as_dummy.down.sql": `
		ALTER TABLE accounts
			DROP COLUMN in_maintenance;
	`,
}

// DB adds convenience functions on top of gorp.DbMap.
type DB struct {
	gorp.DbMap
}

// SelectBool is analogous to the other SelectFoo() functions from gorp.DbMap
// like SelectFloat, SelectInt, SelectStr, etc.
func (db *DB) SelectBool(query string, args ...any) (bool, error) {
	var result bool
	err := db.QueryRow(query, args...).Scan(&result)
	return result, err
}

// Configuration returns the easypg.Configuration object that func main() needs to initialize the DB connection.
func DBConfiguration() easypg.Configuration {
	return easypg.Configuration{
		Migrations: sqlMigrations,
	}
}

// InitORM wraps a database connection into a gorp.DbMap instance.
func InitORM(dbConn *sql.DB) *DB {
	// ensure that this process does not starve other Keppel processes for DB connections
	dbConn.SetMaxOpenConns(16)

	result := &DB{DbMap: gorp.DbMap{Db: dbConn, Dialect: gorp.PostgresDialect{}}}
	result.DbMap.AddTableWithName(models.Account{}, "accounts").SetKeys(false, "name")
	result.DbMap.AddTableWithName(models.Blob{}, "blobs").SetKeys(true, "id")
	result.DbMap.AddTableWithName(models.Upload{}, "uploads").SetKeys(false, "repo_id", "uuid")
	result.DbMap.AddTableWithName(models.Repository{}, "repos").SetKeys(true, "id")
	result.DbMap.AddTableWithName(models.Manifest{}, "manifests").SetKeys(false, "repo_id", "digest")
	result.DbMap.AddTableWithName(models.Tag{}, "tags").SetKeys(false, "repo_id", "name")
	result.DbMap.AddTableWithName(models.ManifestContent{}, "manifest_contents").SetKeys(false, "repo_id", "digest")
	result.DbMap.AddTableWithName(models.Quotas{}, "quotas").SetKeys(false, "auth_tenant_id")
	result.DbMap.AddTableWithName(models.Peer{}, "peers").SetKeys(false, "hostname")
	result.DbMap.AddTableWithName(models.PendingBlob{}, "pending_blobs").SetKeys(false, "account_name", "digest")
	result.DbMap.AddTableWithName(models.UnknownBlob{}, "unknown_blobs").SetKeys(false, "account_name", "storage_id")
	result.DbMap.AddTableWithName(models.UnknownManifest{}, "unknown_manifests").SetKeys(false, "account_name", "repo_name", "digest")
	result.DbMap.AddTableWithName(models.TrivySecurityInfo{}, "trivy_security_info").SetKeys(false, "repo_id", "digest")

	return result
}
