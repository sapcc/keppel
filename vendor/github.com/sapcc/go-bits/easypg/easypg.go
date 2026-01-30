// SPDX-FileCopyrightText: 2018 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

// Package easypg is a database library for applications that use PostgreSQL.
// It imports the libpq SQL driver and integrates
// github.com/golang-migrate/migrate for data definition.
package easypg

import (
	"database/sql"
	"errors"
	"fmt"
	url "net/url"
	"os"
	"regexp"
	"strings"

	"github.com/sapcc/go-bits/sqlext"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	bindata "github.com/golang-migrate/migrate/v4/source/go_bindata"

	// enable postgres driver for database/sql
	_ "github.com/lib/pq"
)

// Configuration contains settings for Init(). The field Migrations needs to have keys
// matching the filename format expected by github.com/golang-migrate/migrate
// (see documentation there for details), for example:
//
//	cfg.Migrations = map[string]string{
//	    "001_initial.up.sql": `
//	        CREATE TABLE things (
//	            id   BIGSERIAL NOT NULL PRIMARY KEY,
//	            name TEXT NOT NULL,
//	        );
//	    `,
//	    "001_initial.down.sql": `
//	        DROP TABLE things;
//	    `,
//	}
type Configuration struct {
	// (required) The schema migrations, in Postgres syntax. See above for details.
	Migrations map[string]string
	// (optional) If not empty, use this database/sql driver instead of "postgres".
	// This is useful e.g. when using github.com/majewsky/sqlproxy.
	OverrideDriverName string
}

// Connect connects to a Postgres database.
//
// The given URL must be a libpq connection URL, see:
// <https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-CONNSTRING>
//
// We recommend constructing the URL with func URLFrom.
func Connect(dbURL url.URL, cfg Configuration) (*sql.DB, error) {
	migrations := cfg.Migrations
	migrations = wrapDDLInTransactions(migrations)
	migrations = stripWhitespace(migrations)

	// use the "go-bindata" driver for github.com/golang-migrate/migrate
	var assetNames []string
	for name := range migrations {
		assetNames = append(assetNames, name)
	}
	asset := func(name string) ([]byte, error) {
		data, ok := migrations[name]
		if ok {
			return []byte(data), nil
		}
		return nil, &os.PathError{Op: "open", Path: name, Err: errors.New("not found")}
	}

	sourceDriver, err := bindata.WithInstance(bindata.Resource(assetNames, asset))
	if err != nil {
		return nil, err
	}

	db, dbd, err := connectToPostgres(dbURL, cfg.OverrideDriverName)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to Postgres: %w", err)
	}

	err = runMigration(migrate.NewWithInstance("go-bindata", sourceDriver, "postgres", dbd))
	if err != nil {
		return nil, fmt.Errorf("cannot apply database schema: %w", err)
	}
	return db, nil
}

var dbNotExistErrRx = regexp.MustCompile(`^pq: database "([^"]+)" does not exist \(3D000\)$`)

func connectToPostgres(dbURL url.URL, driverName string) (*sql.DB, database.Driver, error) {
	if driverName == "" {
		driverName = "postgres"
	}
	db, err := sql.Open(driverName, dbURL.String())
	if err == nil {
		// apparently the "database does not exist" error only occurs when trying to issue the first statement
		_, err = db.Exec("SELECT 1")
	}
	if err == nil {
		// success
		dbd, err := postgres.WithInstance(db, &postgres.Config{})
		return db, dbd, err
	}
	match := dbNotExistErrRx.FindStringSubmatch(err.Error())
	if match == nil {
		// unexpected error
		return nil, nil, err
	}
	dbName := match[1]

	// connect to Postgres without the database name specified, so that we can
	// execute CREATE DATABASE
	urlWithoutDB := dbURL
	urlWithoutDB.Path = "/"
	db2, err := sql.Open(driverName, urlWithoutDB.String())
	if err == nil {
		_, err = db2.Exec("CREATE DATABASE " + dbName)
	}
	if err == nil {
		err = db2.Close()
	} else {
		db2.Close()
	}
	if err != nil {
		return nil, nil, err
	}

	// now the actual database is there and we can connect to it
	db, err = sql.Open(driverName, dbURL.String())
	if err != nil {
		return nil, nil, err
	}
	dbd, err := postgres.WithInstance(db, &postgres.Config{})
	return db, dbd, err
}

func runMigration(m *migrate.Migrate, err error) error {
	if err != nil {
		return err
	}
	err = m.Up()
	if errors.Is(err, migrate.ErrNoChange) {
		// no idea why this is an error
		return nil
	}
	return err
}

func stripWhitespace(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for filename, sql := range in {
		sqlSimplified := sqlext.SimplifyWhitespace(sql)
		out[filename] = strings.ReplaceAll(sqlSimplified, "; ", ";\n")
	}
	return out
}

func wrapDDLInTransactions(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for filename, sql := range in {
		// wrap DDL in transactions
		out[filename] = "BEGIN;\n" + strings.TrimSuffix(strings.TrimSpace(sql), ";") + ";\nCOMMIT;"
	}
	return out
}
