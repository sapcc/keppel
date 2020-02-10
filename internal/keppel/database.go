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
