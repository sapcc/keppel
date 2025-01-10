/******************************************************************************
*
*  Copyright 2024 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package easypg

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	url "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/must"
	"github.com/sapcc/go-bits/sqlext"
)

// this custom port avoids conflicts with any system-wide Postgres instances on the standard port 5432
const testDBPort = 54320

var clientLaunchScript = `#!/usr/bin/env bash
set -euo pipefail

stop_postgres() {
	EXIT_CODE=$?
	pg_ctl stop --wait --silent -D .testdb/datadir
	exit "${EXIT_CODE}"
}
trap stop_postgres EXIT INT TERM

rm -f -- .testdb/run/postgresql.log
pg_ctl start --wait --silent -D .testdb/datadir -l .testdb/run/postgresql.log
%[1]s -U postgres -h 127.0.0.1 -p %[2]d "$@"
`

var hasTestDB = false

// WithTestDB spawns a PostgreSQL database for the duration of a `go test` run.
// Its data directory, configuration and logs are stored in the ".testdb" directory below the repository root.
//
// How to interact with the test database:
//   - To inspect it manually, use one of the helper scripts in the ".testdb" directory, e.g. ".testdb/psql.sh".
//   - It is currently not supported to run tests for multiple packages concurrently, so make sure to run "go test" with "-p 1".
//   - The "/.testdb" directory should be added to your repository's .gitignore rules.
//
// This function takes a testing.M because it is supposed to be called from TestMain().
// This is required to ensure that its cleanup phase shuts down the database server after all tests have been executed.
// Add a TestMain() like this to each package that needs to interact with the test database:
//
//	func TestMain(m *testing.M) {
//		easypg.WithTestDB(m, func() int { return m.Run() })
//	}
//
// This function will fail when running as root (which might happen in some Docker containers), because PostgreSQL refuses to run as UID 0.
func WithTestDB(m *testing.M, action func() int) int {
	rootPath := must.Return(findRepositoryRootDir())

	// create DB on first use
	hasPostgresDB := must.Return(checkPathExists(filepath.Join(rootPath, ".testdb/datadir/PG_VERSION")))
	if !hasPostgresDB {
		for _, dirName := range []string{".testdb/datadir", ".testdb/run"} {
			must.Succeed(os.MkdirAll(filepath.Join(rootPath, dirName), 0777)) // subject to umask
		}
		cmd := exec.Command("initdb", "-A", "trust", "-U", "postgres", //nolint:gosec // rule G204 is overly broad
			"-D", filepath.Join(rootPath, ".testdb/datadir"),
			"-c", "external_pid_file="+filepath.Join(rootPath, ".testdb/run/pid"),
			"-c", "unix_socket_directories="+filepath.Join(rootPath, ".testdb/run"),
			"-c", fmt.Sprintf("port=%d", testDBPort),
		)
		cmd.Stdin = nil
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			logg.Fatal("could not run initdb: %s", err.Error())
		}
	}

	// check if a previous connection is still lingering
	if _, err := os.Stat(filepath.Join(rootPath, ".testdb/run/pid")); err == nil {
		err := stopDatabaseServer(rootPath)
		if err != nil {
			logg.Error(err.Error())
		}
	}

	// drop helper scripts that can be used to attach to the test DB for manual debugging and inspection
	for _, clientTool := range []string{"psql", "pgcli", "pg_dump"} {
		path := filepath.Join(rootPath, ".testdb", clientTool+".sh")
		contents := fmt.Sprintf(clientLaunchScript, clientTool, testDBPort)
		must.Succeed(os.WriteFile(path, []byte(contents), 0777)) // subject to umask, intentionally executable
	}

	// start database process
	cmd := exec.Command("pg_ctl", "start", "--wait", "--silent", //nolint:gosec // rule G204 is overly broad
		"-D", filepath.Join(rootPath, ".testdb/datadir"),
		"-l", filepath.Join(rootPath, ".testdb/run/postgresql.log"),
	)
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		logg.Fatal("could not run pg_ctl start: %s", err.Error())
	}

	// run tests
	hasTestDB = true
	exitCode := action()
	hasTestDB = false

	// stop database process (regardless of whether tests succeeded or failed!)
	err = stopDatabaseServer(rootPath)
	if err != nil {
		logg.Fatal(err.Error())
	}

	return exitCode
}

func stopDatabaseServer(rootPath string) error {
	cmd := exec.Command("pg_ctl", "stop", "--wait", "--silent", //nolint:gosec // rule G204 is overly broad
		"-D", filepath.Join(rootPath, ".testdb/datadir"),
	)
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("could not run pg_ctl stop: %w", err)
	}
	return nil
}

func findRepositoryRootDir() (string, error) {
	// NOTE: `go test` runs each test within the directory containing its source code.
	dirPath, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		isRepoRoot, err := checkPathExists(filepath.Join(dirPath, "go.mod"))
		switch {
		case err != nil:
			return "", err
		case isRepoRoot:
			return dirPath, nil
		default:
			// this is not the repo root, keep searching
			parentPath := filepath.Dir(dirPath)
			if parentPath == dirPath {
				return "", errors.New("could not find repository root (neither $PWD nor any parents contain a go.mod file)")
			}
			dirPath = parentPath
		}
	}
}

func checkPathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case os.IsNotExist(err):
		return false, nil
	default:
		return false, err
	}
}

type testSetupParams struct {
	databaseName          string
	sqlStatementsForClear []string
	tableNamesForClear    []string
	sqlFileToLoad         string
	tableNamesForPKReset  []string
}

// TestSetupOption is an optional behavior that can be given to ConnectForTest().
type TestSetupOption func(*testSetupParams)

// ClearContentsWith is a TestSetupOption that removes records from the DB using the provided SQL statement.
// If provided, this runs directly after connecting, before any other setup phase.
// The provided SQL statement is executed repeatedly, until result.RowsAffected() == 0 is observed.
//
// Prefer ClearTables() over this, and only use this if ClearTables() does not work.
func ClearContentsWith(sqlStatement string) TestSetupOption {
	return func(params *testSetupParams) {
		params.sqlStatementsForClear = append(params.sqlStatementsForClear, sqlext.SimplifyWhitespace(sqlStatement))
	}
}

// ClearTables is a TestSetupOption that removes all rows from the given tables.
//
// This option only works for tables that can be cleared with `DELETE FROM <table>`.
// If specific setups like "ON DELETE RESTRICT" constraints make that impossible,
// use ClearContentsWith() to provide a specialized clearing method that runs before ClearTables().
func ClearTables(tableNames ...string) TestSetupOption {
	return func(params *testSetupParams) {
		params.tableNamesForClear = append(params.tableNamesForClear, tableNames...)
	}
}

// LoadSQLFile is a TestSetupOption that loads a file containing SQL statements and executes them all.
// Every SQL statement must be on a single line.
//
// This executes after any ClearTables() options, but before any ResetPrimaryKeys() options.
func LoadSQLFile(path string) TestSetupOption {
	return func(params *testSetupParams) {
		params.sqlFileToLoad = path
	}
}

// ResetPrimaryKeys is a TestSetupOption that resets the sequences for the "id"
// column of the given tables to start at 1 again (or if there are entries in
// the table, to start right after the entry with the highest ID).
func ResetPrimaryKeys(tableNames ...string) TestSetupOption {
	return func(params *testSetupParams) {
		params.tableNamesForPKReset = append(params.tableNamesForPKReset, tableNames...)
	}
}

// OverrideDatabaseName is a TestSetupOption that picks a different database
// name than the default of t.Name().
//
// This is only necessary if a single test needs to use multiple database connections at the same time,
// e.g. to simulate two separate deployments of the application next to each other.
func OverrideDatabaseName(dbName string) TestSetupOption {
	return func(params *testSetupParams) {
		params.databaseName = dbName
	}
}

// ConnectForTest connects to the test database server managed by func WithTestDB().
// Any number of TestSetupOption arguments can be given to reset and prepare the database for the test run.
//
// Each test will run in its own separate database (whose name is the same as the test name),
// so it is safe to mark tests as t.Parallel() to run multiple tests within the same package concurrently.
func ConnectForTest(t TestingT, cfg Configuration, opts ...TestSetupOption) *sql.DB {
	t.Helper()

	var params testSetupParams
	for _, o := range opts {
		o(&params)
	}

	// input validation
	if !hasTestDB {
		t.Fatal("easypg.ConnectForTest() can only be used if easypg.WithTestDB() was called in TestMain (see docs on func WithTestDB for details)")
	}

	// connect to DB (the database name is set to the test name to isolate concurrent tests from each other)
	dbName := normalizeDBName(t.Name())
	if params.databaseName != "" {
		dbName = normalizeDBName(params.databaseName)
	}
	dbURLStr := fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/%s?sslmode=disable", testDBPort, dbName)
	dbURL, err := url.Parse(dbURLStr)
	if err != nil {
		t.Fatalf("malformed database URL %q: %s", dbURLStr, err.Error())
	}
	db, err := Connect(*dbURL, cfg)
	if err != nil {
		t.Fatal(err.Error())
	}

	// execute ClearContentsWith() setup options, if any
	for _, sqlStatement := range params.sqlStatementsForClear {
		for {
			result, err := db.Exec(sqlStatement)
			if err != nil {
				t.Fatalf("while clearing contents with %q: %s", sqlStatement, err.Error())
			}
			rowCount, err := result.RowsAffected()
			if err != nil {
				t.Fatalf("while clearing contents with %q: %s", sqlStatement, err.Error())
			}
			if rowCount == 0 {
				break
			}
		}
	}

	// execute ClearTables() setup option, if any
	for _, tableName := range params.tableNamesForClear {
		_, err := db.Exec(fmt.Sprintf(`DELETE FROM "%s"`, tableName))
		if err != nil {
			t.Fatalf("while clearing table %s: %s", tableName, err.Error())
		}
	}

	// execute ExecSQLFile() setup option, if any
	if params.sqlFileToLoad != "" {
		sqlBytes, err := os.ReadFile(params.sqlFileToLoad)
		if err != nil {
			t.Fatal(err.Error())
		}

		// split into single statements because db.Exec() will just ignore everything after the first semicolon
		for idx, line := range strings.Split(string(sqlBytes), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "--") {
				continue
			}
			_, err = db.Exec(line)
			if err != nil {
				t.Fatalf("error in %s on line %d: %s", params.sqlFileToLoad, idx, err.Error())
			}
		}
	}

	// execute ResetPrimaryKeys() setup option, if any
	for _, tableName := range params.tableNamesForPKReset {
		var nextID int64
		query := fmt.Sprintf(`SELECT 1 + COALESCE(MAX(id), 0) FROM "%s"`, tableName) //nolint:gosec // we are just using it for tests
		err := db.QueryRow(query).Scan(&nextID)
		if err != nil {
			t.Fatalf("while checking IDs in table %s: %s", tableName, err.Error())
		}

		query = fmt.Sprintf(`ALTER SEQUENCE %s_id_seq RESTART WITH %d`, tableName, nextID)
		_, err = db.Exec(query)
		if err != nil {
			t.Fatalf("while resetting ID sequence on table %s: %s", tableName, err.Error())
		}
	}

	return db
}

var dbNameForbiddenCharsRx = regexp.MustCompile(`[^a-z0-9_]+`)

func normalizeDBName(input string) string {
	input = strings.ToLower(input)

	// With regular Go tests, the test names are function identifiers, and these work well as DB names too.
	// But in Ginkgo, the test names are built from all the context around it. For example:
	//
	//	var _ = Describe("ObjectManager", func() {
	//		When("stuff happens", func() {
	//			It("does a thing", func() {
	//				fmt.Println(GinkgoT().Name())
	//			})
	//		})
	//	})
	//
	// This will report a test name of "ObjectManager when stuff happens does a thing".
	// Whitespace (as well as most special characters) are forbidden in database names,
	// so we need to normalize this quite a bit more.
	input = dbNameForbiddenCharsRx.ReplaceAllString(input, "_")

	// In heavily nested Ginkgo test suites, test names can therefore also be very long.
	// When the max length for Postgres DB names is exceeded, we need to truncate while maintaining uniqueness.
	const (
		maxLength  = 63
		hashLength = 8
	)
	if len(input) > maxLength {
		hash := sha256.Sum256([]byte(input))
		return fmt.Sprintf("%s_%s",
			strings.ToValidUTF8(input[0:(maxLength-hashLength-1)], ""), // the first 63-(N+1) chars of the test name
			hex.EncodeToString(hash[:])[0:hashLength],                  // N chars of hash
		)
	}

	return input
}
