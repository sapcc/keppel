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
