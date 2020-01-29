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
			name           TEXT NOT NULL PRIMARY KEY,
			auth_tenant_id TEXT NOT NULL
		);
	`,
	"001_initial.down.sql": `
		DROP TABLE accounts;
	`,
	"002_add_rbac.up.sql": `
		CREATE TABLE rbac_policies (
			account_name     TEXT    NOT NULL REFERENCES accounts ON DELETE CASCADE,
			match_repository TEXT    NOT NULL,
			match_username   TEXT    NOT NULL,
			can_anon_pull    BOOLEAN NOT NULL DEFAULT FALSE,
			can_pull         BOOLEAN NOT NULL DEFAULT FALSE,
			can_push         BOOLEAN NOT NULL DEFAULT FALSE,
			PRIMARY KEY (account_name, match_repository, match_username)
		);
	`,
	"002_add_rbac.down.sql": `
		DROP TABLE rbac_policies;
	`,
	"003_add_registry_secret.up.sql": `
		ALTER TABLE accounts ADD COLUMN registry_http_secret TEXT NOT NULL DEFAULT '';
	`,
	"003_add_registry_secret.down.sql": `
		ALTER TABLE accounts DROP COLUMN registry_http_secret;
	`,
	"004_add_rbac_can_delete.up.sql": `
		ALTER TABLE rbac_policies ADD COLUMN can_delete BOOLEAN NOT NULL DEFAULT FALSE;
	`,
	"004_add_rbac_can_delete.down.sql": `
		ALTER TABLE rbac_policies DROP COLUMN can_delete;
	`,
	//NOTE: The `repos` table is not strictly necessary. We could use
	//(account_name, repo_name) instead of repo_id in `manifests` and `tags`.
	//Giving numerical IDs to repos is just a storage space optimization.
	"005_add_repos_manifests_tags.up.sql": `
		CREATE TABLE repos (
			id           BIGSERIAL NOT NULL PRIMARY KEY,
			account_name TEXT      NOT NULL REFERENCES accounts ON DELETE CASCADE,
			name         TEXT      NOT NULL
		);
		CREATE TABLE manifests (
			repo_id    BIGINT NOT NULL REFERENCES repos ON DELETE CASCADE,
			digest     TEXT   NOT NULL,
			media_type TEXT   NOT NULL,
			size_bytes BIGINT NOT NULL,
			PRIMARY KEY (repo_id, digest)
		);
		CREATE TABLE tags (
			repo_id    BIGINT NOT NULL REFERENCES repos ON DELETE CASCADE,
			name       TEXT   NOT NULL,
			digest     TEXT   NOT NULL,
			PRIMARY KEY (repo_id, name),
			FOREIGN KEY (repo_id, digest) REFERENCES manifests ON DELETE CASCADE
		);
	`,
	"005_add_repos_manifests_tags.down.sql": `
		DROP TABLE repos;
		DROP TABLE manifests;
		DROP TABLE tags;
	`,
	"006_add_pushed_at_timestamps.up.sql": `
		ALTER TABLE manifests ADD COLUMN pushed_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
		ALTER TABLE tags ADD COLUMN pushed_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
	`,
	"006_add_pushed_at_timestamps.down.sql": `
		ALTER TABLE manifests DROP COLUMN pushed_at;
		ALTER TABLE tags DROP COLUMN pushed_at;
	`,
	"007_add_quotas.up.sql": `
		CREATE TABLE quotas (
			auth_tenant_id TEXT   NOT NULL PRIMARY KEY,
			manifests      BIGINT NOT NULL
		);
	`,
	"007_add_quotas.down.sql": `
		DROP TABLE quotas;
	`,
	"008_add_peers.up.sql": `
		CREATE TABLE peers (
			hostname                     TEXT        NOT NULL PRIMARY KEY,
			our_password                 TEXT        NOT NULL DEFAULT '',
			their_current_password_hash  TEXT        NOT NULL DEFAULT '',
			their_previous_password_hash TEXT        NOT NULL DEFAULT '',
			last_peered_at               TIMESTAMPTZ DEFAULT NULL
		);
	`,
	"008_add_peers.down.sql": `
		DROP TABLE peers;
	`,
	"009_add_replication_on_first_use.up.sql": `
		ALTER TABLE accounts ADD COLUMN upstream_peer_hostname TEXT NOT NULL DEFAULT '';
	`,
	"009_add_replication_on_first_use.down.sql": `
		ALTER TABLE accounts DROP COLUMN upstream_peer_hostname;
	`,
	"010_add_pending_blobs.up.sql": `
		CREATE TABLE pending_blobs (
			repo_id BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
			digest  TEXT        NOT NULL,
			reason  TEXT        NOT NULL,
			since   TIMESTAMPTZ DEFAULT NOW(),
			PRIMARY KEY (repo_id, digest)
		);
	`,
	"010_add_pending_blobs.down.sql": `
		DROP TABLE pending_blobs;
	`,
	"011_add_pending_manifests.up.sql": `
		CREATE TABLE pending_manifests (
			repo_id    BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
			reference  TEXT        NOT NULL,
			digest     TEXT        NOT NULL,
			reason     TEXT        NOT NULL,
			since      TIMESTAMPTZ DEFAULT NOW(),
			media_type TEXT        NOT NULL,
			content    TEXT        NOT NULL,
			PRIMARY KEY (repo_id, reference)
		);
	`,
	"011_add_pending_manifests.down.sql": `
		DROP TABLE pending_manifests;
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

//IsStillReachable implements the DBAccessForOrchestrationDriver interface.
//It checks if the given DB can still execute SQL queries.
func (db *DB) IsStillReachable() bool {
	val, err := db.SelectInt(`SELECT 42`)
	if err != nil {
		logg.Error("DB has become unreachable: " + err.Error())
		return false
	}
	if val != 42 {
		logg.Error("DB has become unreachable")
		return false
	}
	return true
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
