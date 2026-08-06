// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package pgruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"fmt"
	"regexp"
	"strings"

	"go.xyrillian.de/gg/assert"
	"go.xyrillian.de/gg/errext"
	"go.xyrillian.de/gg/gsql"
)

// Connector describes how to connect to a PostgreSQL database given a [libpq-style connection URI].
// This type acts as a dependency injection surface, abstracting the different ways in which different database drivers and libraries perform connection.
//
// [libpq-style connection URI]: https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-CONNSTRING-URIS
type Connector[T gsql.ConnectionHandle] func(context.Context, ConnectionTarget) (T, error)

// StdConnector returns a [Connector] for database/sql drivers.
// When used with [lib/pq], the driver name must be "postgres".
func StdConnector(driverName string) Connector[*gsql.DB] {
	return func(ctx context.Context, target ConnectionTarget) (*gsql.DB, error) {
		u, err := target.IntoURL()
		if err != nil {
			return nil, err
		}
		db, err := sql.Open(driverName, u.String())
		if err != nil {
			return nil, err
		}
		return gsql.NewDB(db), nil
	}
}

// Connect connects to a PostgreSQL database.
func (c Connector[T]) Connect(ctx context.Context, target ConnectionTarget, behavior ConnectionBehavior) (T, error) {
	var none T // shorthand for error return paths

	db, err := c(ctx, target)
	if err != nil {
		return none, err
	}
	err = behavior.applyTo(ctx, db)
	if err != nil {
		return none, errext.WithCleanup(err, "db.Close", db.GSQLClose(ctx))
	}
	return db, nil
}

// TestSetupOption is an optional behavior that can be given to [Connector.ConnectForTest].
type TestSetupOption func(*testSetupParams)

type testSetupParams struct {
	DatabaseName string
}

// OverrideDatabaseName is a [TestSetupOption] that picks a different database name than the default of t.Name().
//
// This is only necessary if a single test needs to use multiple database connections at the same time,
// e.g. to simulate two separate deployments of the application next to each other.
func OverrideDatabaseName(dbName string) TestSetupOption {
	return func(params *testSetupParams) {
		params.DatabaseName = dbName
	}
}

// ConnectForTest connects to the test database server managed by [WithTestDB].
//
// Each test will run in its own separate database (whose name is the same as t.Name()),
// so it is safe to mark tests as t.Parallel() to run multiple tests within the same package concurrently.
// ConnectForTest will always drop and recreate the database to ensure deterministic test behavior.
//
// Some tests require setting up multiple separate connections to the same database.
// The second return argument can be be used with [Connector.Connect] to obtain additional connections as needed.
func (c Connector[T]) ConnectForTest(t assert.TestingTB, behavior ConnectionBehavior, opts ...TestSetupOption) (T, ConnectionTarget) {
	t.Helper()
	ctx := t.Context()

	params := testSetupParams{
		DatabaseName: t.Name(),
	}
	for _, opt := range opts {
		opt(&params)
	}

	// normalize t.Name() into an acceptable database name for PostgreSQL
	//   - only alphanumerics and underscore -> replace all other symbols with _
	//   - max 63 chars -> if overflown, truncate and append a short digest to hopefully make it unique
	dbName := strings.ToLower(params.DatabaseName)
	dbName = regexp.MustCompile(`[^a-z_]`).ReplaceAllString(dbName, "_")
	if len(dbName) > 63 {
		digest := sha256.Sum256([]byte(params.DatabaseName))
		encoded := base32.HexEncoding.EncodeToString(digest[:])
		dbName = dbName[0:53] + "__" + strings.ToLower(encoded[0:8])
	}

	// connect to "postgres" database for the CREATE DATABASE query (if necessary)
	target := ConnectionTarget{
		HostName:          "127.0.0.1",
		Port:              testdbPort,
		UserName:          testdbUserName,
		DatabaseName:      "postgres",
		ConnectionOptions: "sslmode=disable",
	}
	adminDB, err := c(ctx, target)
	if err != nil {
		t.Fatal(err.Error() + " (if this error is about the database server not running, check if your TestMain() calls pgruntime.WithTestDB())")
	}
	err = createDatabaseIfMissing(ctx, adminDB, dbName)
	err = errext.WithCleanup(err, "db.Close", adminDB.GSQLClose(ctx))
	if err != nil {
		t.Fatal(err.Error())
	}

	// connect to actual test database
	target.DatabaseName = dbName
	testDB, err := c.Connect(ctx, target, behavior)
	if err != nil {
		t.Fatal(err.Error())
	}
	err = resetTestDatabase(ctx, testDB, params, behavior)
	if err != nil {
		err = errext.WithCleanup(err, "db.Close", testDB.GSQLClose(ctx))
		t.Fatal(err.Error())
	}
	return testDB, target
}

func createDatabaseIfMissing(ctx context.Context, db gsql.Handle, dbName string) error {
	// check if database exists
	exists, err := selectOneValue[bool](ctx, db, `SELECT COUNT(*) > 0 FROM pg_catalog.pg_database WHERE datname = $1`, dbName)
	if err != nil {
		return fmt.Errorf("while reading from pg_catalog.pg_database: %w", err)
	}

	// create database if necessary
	if !exists {
		_, err = execQuery(ctx, db, "CREATE DATABASE "+quoteIdentifier(dbName), nil)
		if err != nil {
			return fmt.Errorf("during CREATE DATABASE: %w", err)
		}
	}

	return nil
}

func resetTestDatabase(ctx context.Context, db gsql.Handle, params testSetupParams, behavior ConnectionBehavior) error {
	// enumerate all tables that need to be truncated (all tables that are not managed by pgruntime)
	condition := `table_schema = 'public' AND table_type = 'BASE TABLE'`
	if len(behavior.Migrations) > 0 {
		condition += ` AND table_name != 'schema_migrations'`
	}
	query := fmt.Sprintf(`SELECT quote_ident(table_name) FROM information_schema.tables WHERE %s ORDER BY table_name`, condition)
	quotedTableNames, err := selectSeveralValues[string](ctx, db, query)
	if err != nil {
		return fmt.Errorf("while listing tables to truncate: %w", err)
	}

	// truncate all tables at once
	if len(quotedTableNames) > 0 {
		query = fmt.Sprintf(`TRUNCATE %s RESTART IDENTITY CASCADE`, strings.Join(quotedTableNames, ", "))
		_, err = execQuery(ctx, db, query, nil)
		if err != nil {
			return fmt.Errorf("during %s: %w", query, err)
		}
	}

	return nil
}
