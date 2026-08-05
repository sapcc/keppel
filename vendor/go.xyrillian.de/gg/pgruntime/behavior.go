// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package pgruntime

import (
	"context"
	"fmt"
	"slices"

	"go.xyrillian.de/gg/gsql"
)

// ConnectionBehavior contains configuration for [Connector.Connect] and [Connector.ConnectForTest].
//
// If Migrations is not nil, pgruntime will perform very basic handling for schema migrations.
// Migrations must be given with the version number as key, and one or several DDL queries needed to reach that version from the previous version.
// For example:
//
//	behavior.Migrations = map[int]string{
//		1: `
//			CREATE TABLE assets (
//				id   BIGSERIAL PRIMARY KEY,
//				name TEXT
//			);
//		`,
//		2: `
//			UPDATE assets SET name = 'unknown' WHERE name IS NULL;
//			ALTER TABLE assets ALTER COLUMN name SET NOT NULL;
//		`,
//	}
//
// Versions need not start at 1 and need not be contiguous (so e.g. UNIX timestamps can be used as schema versions).
// Schema migrations will be executed as follows:
//
//   - The table "schema_migrations" will be created with the same schema as used by [golang-migrate] if it does not exists (see [MigrationsSchema]).
//     It will only ever contain one record, initially with version 0.
//   - If Migrations contains entries with a version number larger than the one recorded in the database,
//     pgruntime picks the first such migration by ascending version number, executes the DDL query, and increases the version number in "schema_migrations" accordingly.
//
// [golang-migrate]: https://pkg.go.dev/github.com/golang-migrate/migrate
type ConnectionBehavior struct {
	Migrations map[int64]string // or nil to skip schema_migrations
}

// MigrationsSchema defines the structure of the "schema_migrations" table used by pgruntime's schema migration handling.
// It is the same table schema as used by [golang-migrate]'s postgres driver, to enable seamless migration from that library to pgruntime.
// The "dirty" column is not used by pgruntime, and will always be set to FALSE for compatibility with golang-migrate.
//
// [golang-migrate]: https://pkg.go.dev/github.com/golang-migrate/migrate
const MigrationsSchema = `CREATE TABLE IF NOT EXISTS schema_migrations (version BIGINT NOT NULL PRIMARY KEY, dirty BOOLEAN NOT NULL)`

func (b ConnectionBehavior) applyTo(ctx context.Context, db gsql.ConnectionHandle) error {
	if len(b.Migrations) > 0 {
		err := applyMigrations(ctx, db, b.Migrations)
		if err != nil {
			return err
		}
	}
	return nil
}

func applyMigrations(ctx context.Context, db gsql.ConnectionHandle, migrations map[int64]string) error {
	// apply schema_migrations table schema
	_, err := execQuery(ctx, db, MigrationsSchema, nil)
	if err != nil {
		return fmt.Errorf("could not apply schema_migrations table schema: %w", err)
	}

	// read schema_migrations table
	rowCount, err := selectOneValue[int64](ctx, db, `SELECT COUNT(*) FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("could not check row count for schema_migrations: %w", err)
	}
	var (
		currentVersion int64
		dirty          bool
	)
	switch rowCount {
	case 0:
		currentVersion = 0
		_, err = execQuery(ctx, db, `INSERT INTO schema_migrations (version, dirty) VALUES (0, FALSE)`, nil)
		if err != nil {
			return fmt.Errorf("could not initialize schema_migrations record: %w", err)
		}
	case 1:
		err = queryRow(ctx, db, `SELECT version, dirty FROM schema_migrations`, nil, []any{&currentVersion, &dirty})
		if err != nil {
			return fmt.Errorf("could not read schema_migrations record: %w", err)
		}
	default:
		if err != nil {
			return fmt.Errorf("expected 1 record in schema_migrations table, but found %d records", rowCount)
		}
	}
	if dirty {
		// NOTE: defense in depth: this can never occur when only using pgruntime, but may occur when migrating from golang-migrate
		return fmt.Errorf("schema_migrations is marked as dirty (version = %d)", currentVersion)
	}

	// find migrations to apply
	var pendingVersions []int64
	for version := range migrations {
		if version > currentVersion {
			pendingVersions = append(pendingVersions, version)
		}
	}
	slices.Sort(pendingVersions)

	// apply migrations
	for _, version := range pendingVersions {
		err := db.GSQLTransact(ctx, func(tx gsql.Handle) error {
			// ensure that nobody else is migrating until we are done
			var actualVersion int64
			err := queryRow(ctx, db, `SELECT version FROM schema_migrations FOR UPDATE`, nil, []any{&actualVersion})
			if err != nil {
				return fmt.Errorf("could not obtain lock for schema migration: %w", err)
			}

			// ensure that nobody else migrated just before we started our transaction block
			if version <= actualVersion {
				return fmt.Errorf("tried to perform migration to version %d, but schema is already at version %d (multiple migrations might be running concurrently)",
					version, actualVersion,
				)
			}

			// perform the next migration
			_, err = execQuery(ctx, db, migrations[version], nil)
			if err != nil {
				return fmt.Errorf("could not execute schema migration: %w", err)
			}
			_, err = execQuery(ctx, db, `UPDATE schema_migrations SET version = $1, dirty = FALSE`, []any{version})
			if err != nil {
				return fmt.Errorf("could not update schema_migrations record: %w", err)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("while migrating to schema version %d: %w", version, err)
		}
	}

	return nil
}
