// SPDX-FileCopyrightText: 2018-2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"context"
	"database/sql"

	"github.com/dlmiddlecote/sqlstats"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/must"
	"github.com/sapcc/go-bits/sqlext"
	"go.xyrillian.de/gg/gsql"
	"go.xyrillian.de/gg/pgruntime"

	// include SQL driver
	_ "github.com/lib/pq"
)

var sqlMigrations = map[int64]string{
	//NOTE: Migrations 1 through 35 have been rolled up into one at 2024-02-26
	// to better represent the current baseline of the DB schema.
	35: `
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
	36: `
		ALTER TABLE accounts
			ADD COLUMN rbac_policies_json TEXT NOT NULL DEFAULT '';
	`,
	37: `
		DROP TABLE rbac_policies;
	`,
	38: `
		ALTER TABLE blobs
			DROP COLUMN validated_at,
			ADD COLUMN next_validation_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
		ALTER TABLE manifests
			DROP COLUMN validated_at,
			ADD COLUMN next_validation_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
	`,
	// Re 039: These indices are used when selecting tasks for BlobValidationJob
	// and ManifestValidationJob. Before we added indices here, those queries
	// were consistently the most expensive by total execution time.
	39: `
		CREATE INDEX ON blobs (next_validation_at);
		CREATE INDEX ON manifests (next_validation_at);
	`,
	// Re 040: index is used by BlobMountSweepJob
	40: `
		CREATE INDEX ON blob_mounts (can_be_deleted_at NULLS FIRST, repo_id);
		CREATE INDEX ON manifests (validation_error_message) WHERE validation_error_message != '';
	`,
	41: `
		ALTER TABLE accounts
			ADD COLUMN is_managed BOOLEAN NOT NULL DEFAULT FALSE,
			ADD COLUMN next_enforcement_at TIMESTAMPTZ DEFAULT NULL;
	`,
	42: `
		ALTER TABLE peers
			ADD COLUMN use_for_pull_delegation BOOLEAN NOT NULL DEFAULT TRUE;
	`,
	43: `
		ALTER TABLE accounts
			ADD COLUMN is_deleting BOOLEAN NOT NULL DEFAULT FALSE,
			ADD COLUMN next_deletion_attempt_at TIMESTAMPTZ DEFAULT NULL;

		UPDATE accounts SET is_deleting = TRUE WHERE in_maintenance;

		ALTER TABLE accounts
			DROP COLUMN in_maintenance,
			DROP COLUMN metadata_json;
	`,
	44: `
		ALTER TABLE accounts
			ADD COLUMN in_maintenance BOOLEAN NOT NULL DEFAULT FALSE;
	`,
	45: `
		ALTER TABLE accounts
			DROP COLUMN in_maintenance;
	`,
	46: `
		ALTER TABLE manifests
			ADD COLUMN annotations_json TEXT NOT NULL DEFAULT '',
			ADD COLUMN artifact_type TEXT NOT NULL DEFAULT '',
			ADD COLUMN subject_digest TEXT NOT NULL DEFAULT '';
	`,
	47: `
		CREATE INDEX ON manifests (repo_id, subject_digest) WHERE subject_digest != '';
	`,
	48: `
		ALTER TABLE accounts
			ADD COLUMN tag_policies_json TEXT NOT NULL DEFAULT '[]';
	`,
	49: `
		ALTER TABLE trivy_security_info
			ADD COLUMN has_enriched_report BOOLEAN NOT NULL DEFAULT FALSE;
		CREATE TABLE unknown_trivy_reports (
			account_name      TEXT        NOT NULL REFERENCES accounts ON DELETE CASCADE,
			repo_name         TEXT        NOT NULL,
			digest            TEXT        NOT NULL,
			format            TEXT        NOT NULL,
			can_be_deleted_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (account_name, repo_name, digest, format)
		);
	`,
	50: `
		ALTER TABLE trivy_security_info
			ADD COLUMN vuln_status_changed_at TIMESTAMPTZ DEFAULT NULL;
	`,
	51: `
		CREATE INDEX ON trivy_security_info (repo_id, digest) WHERE vuln_status <> 'Clean';
	`,
	52: `
		ALTER TABLE accounts ADD COLUMN rule_for_manifest TEXT NOT NULL DEFAULT '';

		-- Splits the comma separated list of labels and builds a CEL conjunction using the Map Key Membership (in) operator
		-- I.e. 'foo,bar,baz' --> 'foo' in labels && 'bar' in labels && 'baz' in labels
		UPDATE accounts
		SET rule_for_manifest = (
			SELECT string_agg(
				format('%L in labels', trim(label)),
				' && '
			)
			FROM unnest(string_to_array(required_labels, ',')) AS label
		)
		WHERE required_labels <> '';

		ALTER TABLE accounts DROP COLUMN required_labels;
	`,
	53: `
		UPDATE trivy_security_info SET vuln_status = 'Pending', next_check_at = NOW() WHERE vuln_status = 'Rotten';
		ALTER TABLE trivy_security_info
			ALTER COLUMN next_check_at DROP NOT NULL,
			ADD CONSTRAINT next_check_at_only_null_when_rotten CHECK ((vuln_status = 'Rotten') = (next_check_at IS NULL));
	`,
	54: `
		ALTER TABLE quotas
			ADD COLUMN bytes BIGINT NOT NULL DEFAULT -1;
	`,
	55: `
		ALTER TABLE pending_blobs
			ADD COLUMN last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
	`,
}

// DBInterface is implemented by both [*gsql.DB] and [*gsql.Tx].
// We are using this interface in function signatures instead of [gsql.Handle] to allow compatibility with go-bits/sqlext methods.
type DBInterface interface {
	gsql.Handle
	sqlext.Executor
}

var (
	// prove documented interface implementations
	_ DBInterface = &gsql.DB{}
	_ DBInterface = &gsql.Tx{}
)

// SelectOneValue executes a query that yields a single row with a single value.
func SelectOneValue[T any](db DBInterface, query string, args ...any) (T, error) {
	var result T
	err := db.QueryRow(query, args...).Scan(&result)
	return result, err
}

// SelectSeveralValues executes a query that yields rows with a single value each.
func SelectSeveralValues[T any](db DBInterface, query string, args ...any) ([]T, error) {
	var result []T
	err := sqlext.ForeachRow(db, query, args, func(rows *sql.Rows) error {
		var value T
		err := rows.Scan(&value)
		if err == nil {
			result = append(result, value)
		}
		return err
	})
	return result, err
}

// DBConfiguration returns the [pgruntime.ConnectionBehavior] object that [InitDB] uses to initialize the DB connection.
// This is exported because test.NewSetup() needs to be able to access it.
func DBConfiguration() pgruntime.ConnectionBehavior {
	return pgruntime.ConnectionBehavior{
		Migrations: sqlMigrations,
	}
}

// InitDB initializes a DB connection for productive use.
// (Tests use the DB connection logic in test.NewSetup() instead.)
func InitDB(ctx context.Context) *gsql.DB {
	target := getDatabaseURLFromEnvironment()
	dbConn := must.Return(pgruntime.StdConnector("postgres").Connect(ctx, target, DBConfiguration()))
	// ensure that this process does not starve other Keppel processes for DB connections
	dbConn.SetMaxOpenConns(16)

	prometheus.MustRegister(sqlstats.NewStatsCollector(target.DatabaseName, dbConn))
	return dbConn
}
