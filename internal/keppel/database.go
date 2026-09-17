// SPDX-FileCopyrightText: 2018-2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"context"
	"database/sql"
	_ "embed"

	"github.com/dlmiddlecote/sqlstats"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/must"
	"github.com/sapcc/go-bits/sqlext"
	"go.xyrillian.de/gg/gsql"
	"go.xyrillian.de/gg/pgruntime"

	// include SQL driver
	_ "github.com/lib/pq"
)

//go:embed database-baseline.sql
var sqlBaseline string

var sqlMigrations = map[int64]string{
	//NOTE: Migrations 1 through 59 have been rolled up into one at 2026-09-17
	// to better represent the current baseline of the DB schema.
	59: sqlBaseline,
	60: `
		ALTER TABLE manifest_blob_refs
			DROP CONSTRAINT manifest_blob_refs_blob_id_repo_id_fkey;
		ALTER TABLE blob_mounts
			DROP CONSTRAINT blob_mounts_blob_id_repo_id_key,
			ADD PRIMARY KEY (blob_id, repo_id);
		ALTER TABLE manifest_blob_refs
			ADD FOREIGN KEY (blob_id, repo_id) REFERENCES blob_mounts (blob_id, repo_id) ON DELETE RESTRICT;
	`,
	61: `
		ALTER TABLE manifest_contents
			DROP CONSTRAINT manifest_contents_repo_id_digest_key,
			ADD PRIMARY KEY (repo_id, digest);
	`,
	62: `
		ALTER TABLE manifest_blob_refs
			DROP CONSTRAINT manifest_blob_refs_repo_id_digest_blob_id_key,
			ADD PRIMARY KEY (repo_id, digest, blob_id);
	`,
	63: `
		ALTER TABLE manifest_manifest_refs
			DROP CONSTRAINT manifest_manifest_refs_repo_id_parent_digest_child_digest_key,
			ADD PRIMARY KEY (repo_id, parent_digest, child_digest);
	`,
	64: `
		ALTER TABLE trivy_security_info
			DROP CONSTRAINT trivy_security_info_repo_id_digest_key,
			ADD PRIMARY KEY (repo_id, digest);
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
