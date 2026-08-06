// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package pgruntime

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	. "go.xyrillian.de/gg/option"
)

const (
	// ConnectionTarget attributes for test DB
	testdbUserName = "postgres"
	testdbPort     = "54320"

	// paths below $MODULE_ROOT/.testdb
	testdbDatadirLocation = "datadir"
	testdbVersionLocation = "datadir/PG_VERSION"
	testdbRuntimeLocation = "run"
	testdbLogfileLocation = "run/postgresql.log"
	testdbPidfileLocation = "run/pid"
)

//go:embed tool_template.sh
var toolScriptTemplate []byte

// WithTestDB spawns a PostgreSQL database for the duration of a `go test` run.
// Its data directory, configuration and logs are stored in the ".testdb" directory below the module root dir (next to the "go.mod" file).
//   - To inspect it manually, use one of the helper scripts in the ".testdb" directory, e.g. ".testdb/psql.sh".
//   - It is currently not supported to run tests for multiple packages concurrently. Run "go test" with "-p 1" when running tests for multiple packages.
//   - The "/.testdb" directory should be added to your repository's .gitignore rules.
//
// This function takes a [testing.M] because it is supposed to be called from TestMain().
// This ensures that its cleanup phase shuts down the database server after all tests have been executed.
// Add a TestMain() like this to each package that calls [Connector.ConnectForTest]:
//
//	func TestMain(m *testing.M) {
//		pgruntime.WithTestDB(m, m.Run)
//	}
//
// If the environment variable PGRUNTIME_TESTDB_PATH is set, this path will be used instead of ".testdb" below the module root dir.
// This can be used e.g. to place the testdb on a tmpfs to avoid slow disk IO.
// Doing so can reduce test execution times in realistic scenarios by as much as two thirds.
// For clarity, a symlink will be created at the usual ".testdb" location, pointing to the actual path.
//
// If the value of $PGRUNTIME_TESTDB_PATH contains the string "%MODULE%" (e.g. "/tmp/testdb/%MODULE%"),
// this string will be replaced by the name of the main module.
// Using this placeholder, you can set this variable in your shell rc file once,
// but the test databases of different applications will still be neatly separated.
func WithTestDB(m *testing.M, action func() int) int {
	// NOTE: Lifting the "-p 1" restriction is tough because tests for multiple packages are
	//       compiled into one test binary per package, and thus execute in separate processes.
	//       We would need some way for all these binaries to coordinate on only shutting down
	//       the server once everyone is finished with their tests. The obvious choices for
	//       coordination mechanisms (IPC or file locking) are platform-specific and would
	//       require introducing dependencies (ugh) or adding platform-specific code (ugh).

	result, err := withTestDB(m, action)
	if err == nil {
		return result
	} else {
		panic(err.Error())
	}
}

func withTestDB(m *testing.M, action func() int) (_ int, returnedError error) {
	testdbPath, err := findTestdbPath()
	if err != nil {
		return 0, err
	}
	err = stopDBIfLingering(testdbPath)
	if err != nil {
		return 0, err
	}
	err = wipeDBIfMajorUpgrade(testdbPath)
	if err != nil {
		return 0, err
	}
	err = initDBIfNecessary(testdbPath)
	if err != nil {
		return 0, err
	}
	err = startDB(testdbPath)
	if err != nil {
		return 0, err
	}
	defer func() {
		returnedError = stopDB(testdbPath)
	}()
	return action(), nil
}

func findTestdbPath() (string, error) {
	// find `$REPO_ROOT/.testdb`
	cwd, err := os.Getwd()
	if err == nil {
		cwd, err = filepath.Abs(cwd)
	}
	if err != nil {
		return "", fmt.Errorf("could not find working directory: %w", err)
	}
	result, err := findModuleRootDir(cwd)
	if err != nil {
		return "", fmt.Errorf("could not find module root directory: %w", err)
	}
	rootPath, ok := result.Unpack()
	if !ok {
		return "", fmt.Errorf("neither the working directory %q nor any of its parents contain a go.mod file", cwd)
	}
	testdbPath := filepath.Join(rootPath, ".testdb")

	// find override path, if any
	overridePath := os.Getenv("PGRUNTIME_TESTDB_PATH")
	if overridePath == "" {
		return testdbPath, nil
	}
	if strings.Contains(overridePath, "%MODULE%") {
		info, ok := debug.ReadBuildInfo()
		if !ok {
			panic("$PGRUNTIME_TESTDB_PATH contains %MODULE% placeholder, but binary was not built with module support")
		}
		overridePath = strings.ReplaceAll(overridePath, "%MODULE%", info.Main.Path)
	}

	// if there is an override path, report the override path (so that initDBIfNecessary can create it),
	// but put a symlink at the standard path for convenient access
	err = os.Symlink(overridePath, testdbPath)
	if os.IsExist(err) {
		// do not complain if the symlink already exists
		err = nil
	}
	return overridePath, err
}

func findModuleRootDir(dirPath string) (Option[string], error) {
	_, err := os.Stat(filepath.Join(dirPath, "go.mod"))
	switch {
	case err == nil:
		return Some(dirPath), nil
	case os.IsNotExist(err):
		parentPath := filepath.Dir(dirPath)
		if parentPath == dirPath {
			return None[string](), nil
		} else {
			return findModuleRootDir(parentPath)
		}
	default:
		return None[string](), err
	}
}

func wipeDBIfMajorUpgrade(testdbPath string) error {
	// check datadir/PG_VERSION for test database
	buf, err := os.ReadFile(filepath.Join(testdbPath, testdbVersionLocation))
	switch {
	case err == nil:
		// continue below
	case os.IsNotExist(err):
		// DB not initialized yet -> nothing to do
		return nil
	default:
		return err
	}
	serverVersion := strings.TrimSpace(string(buf))

	// check installed PostgreSQL version via `psql --version`
	cmd := exec.Command("psql", "--version")
	cmd.Stderr = os.Stderr
	buf, err = cmd.Output()
	if err != nil {
		return fmt.Errorf("could not run `psql --version`: %w", err)
	}

	// output from `psql --version` should look like e.g.
	//   "psql (PostgreSQL) 18.4"                                (seen on Arch Linux)
	//   "psql (PostgreSQL) 18.4 (Ubuntu 18.4-0ubuntu0.26.04.1)" (seen on Ubuntu)
	// -> we want just the version number part
	clientOutput := strings.TrimSpace(string(buf))
	fields := strings.Fields(clientOutput)
	if len(fields) < 3 || fields[0] != "psql" || fields[1] != "(PostgreSQL)" {
		return fmt.Errorf("unexpected output from `psql --version`: %q", clientOutput)
	}
	clientVersion := fields[2]

	// wipe DB on version mismatch (`serverVersion` will be a major version like "18", and `clientVersion` a minor version like "18.4")
	if strings.HasPrefix(clientVersion, serverVersion) {
		return nil
	}
	return os.RemoveAll(testdbPath) // wipe everything, including the .testdb folder, to make initDBIfNecessary() work
}

func initDBIfNecessary(testdbPath string) error {
	// check if already initialized
	fi, err := os.Stat(testdbPath)
	switch {
	case err == nil:
		if fi.IsDir() {
			return nil
		} else {
			return fmt.Errorf("unexpected type of filesystem entry on %q (expected a directory)", testdbPath)
		}
	case os.IsNotExist(err):
		// need to run initdb, continue below
	default:
		return err
	}

	// create scaffold
	var (
		datadirPath = filepath.Join(testdbPath, testdbDatadirLocation)
		runtimePath = filepath.Join(testdbPath, testdbRuntimeLocation)
	)
	err = os.MkdirAll(datadirPath, 0777)
	if err != nil {
		return err
	}
	err = os.MkdirAll(runtimePath, 0777)
	if err != nil {
		return err
	}
	for _, toolName := range []string{"pgcli", "pg_dump", "psql"} {
		buf := bytes.ReplaceAll(toolScriptTemplate, []byte("$COMMAND"), []byte(toolName))
		err := os.WriteFile(filepath.Join(testdbPath, toolName+".sh"), buf, 0777)
		if err != nil {
			return err
		}
	}

	// run initdb
	cmd := exec.Command("initdb",
		"--pgdata="+datadirPath,
		"--no-locale",
		"--auth=trust", "--username="+testdbUserName,
		"--set", "external_pid_file="+filepath.Join(testdbPath, testdbPidfileLocation),
		"--set", "max_connections=250",
		"--set", "port="+testdbPort,
		"--set", "unix_socket_directories="+runtimePath,
	)
	cmd.Dir = testdbPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("could not run `initdb`: %w", err)
	}
	return nil
}

func startDB(testdbPath string) error {
	// truncate logfile
	logfilePath := filepath.Join(testdbPath, testdbLogfileLocation)
	err := os.Remove(logfilePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// run `pg_ctl start`
	cmd := exec.Command("pg_ctl",
		"start", "--wait", "--silent",
		"-D", filepath.Join(testdbPath, testdbDatadirLocation),
		"-l", logfilePath,
	)
	cmd.Dir = testdbPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("could not run `pg_ctl start`: %w", err)
	}
	return nil
}

func stopDB(testdbPath string) error {
	cmd := exec.Command("pg_ctl",
		"stop", "--wait", "--silent",
		"-D", filepath.Join(testdbPath, testdbDatadirLocation),
	)
	cmd.Dir = testdbPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("could not run `pg_ctl stop`: %w", err)
	}
	return nil
}

func stopDBIfLingering(testdbPath string) error {
	_, err := os.Stat(filepath.Join(testdbPath, testdbPidfileLocation))
	switch {
	case err == nil:
		// looks like a previous instance of this DB is still running -> restart it
		// (we do not just reuse an instance started by someone else because they might terminate it at any point;
		// better to make them fail loudly right now)
		err = stopDB(testdbPath)
		if err != nil {
			return err
		}
		time.Sleep(time.Second / 2) // give them some time to blow up before starting the DB back up
		return nil
	case os.IsNotExist(err):
		return nil // nothing to do, DB is not running
	default:
		return err
	}
}
