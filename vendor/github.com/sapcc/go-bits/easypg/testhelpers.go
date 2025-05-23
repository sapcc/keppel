// SPDX-FileCopyrightText: 2017-2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package easypg

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sapcc/go-bits/osext"
)

// TestingT is implemented by *testing.T, and also satisfied by ginkgo.GinkgoT().
type TestingT interface {
	Fatal(args ...any)
	Fatalf(format string, args ...any)
	Helper()
	Name() string
}

// AssertDBContent makes a dump of the database contents (as a sequence of
// INSERT statements) and runs diff(1) against the given file, producing a test
// error if these two are different from each other.
func AssertDBContent(t TestingT, db *sql.DB, fixtureFile string) {
	t.Helper()
	_, a := NewTracker(t, db)
	a.AssertEqualToFile(fixtureFile)
}

// Tracker keeps a copy of the database contents and allows for checking the
// database contents (or changes made to them) during tests.
type Tracker struct {
	t    TestingT
	db   *sql.DB
	snap dbSnapshot
}

// NewTracker creates a new Tracker.
//
// Since the initial creation involves taking a snapshot, this snapshot is
// returned as a second value. This is an optimization, since it is often
// desired to assert on the full DB contents when creating the tracker. Calling
// Tracker.DBContent() directly after NewTracker() would do a useless second
// snapshot.
func NewTracker(t TestingT, db *sql.DB) (*Tracker, Assertable) {
	t.Helper()
	snap := newDBSnapshot(t, db)
	return &Tracker{t, db, snap}, Assertable{t, snap.ToSQL(nil)}
}

// DBContent produces a dump of the current database contents, as a sequence of
// INSERT statements on which test assertions can be executed.
func (t *Tracker) DBContent() Assertable {
	t.t.Helper()
	t.snap = newDBSnapshot(t.t, t.db)
	return Assertable{t.t, t.snap.ToSQL(nil)}
}

// DBChanges produces a diff of the current database contents against the state
// at the last Tracker call, as a sequence of INSERT/UPDATE/DELETE statements on
// which test assertions can be executed.
func (t *Tracker) DBChanges() Assertable {
	t.t.Helper()
	snap := newDBSnapshot(t.t, t.db)
	diff := snap.ToSQL(t.snap)
	t.snap = snap
	return Assertable{t.t, diff}
}

// Assertable contains a set of SQL statements. Instances are produced by
// methods on type Tracker.
type Assertable struct {
	t       TestingT
	payload string
}

// AssertEqualToFile compares the set of SQL statements to those in the given
// file. A test error is generated in case of differences.
func (a Assertable) AssertEqualToFile(fixtureFile string) {
	a.t.Helper()

	// write actual content to file to make it easy to copy the computed result over
	// to the fixture path when a new test is added or an existing one is modified
	fixturePath, err := filepath.Abs(fixtureFile)
	failOnErr(a.t, err)
	actualPath := fixturePath + ".actual"
	failOnErr(a.t, os.WriteFile(actualPath, []byte(a.payload), 0o666))

	cmd := exec.Command("diff", "-u", fixturePath, actualPath)
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	failOnErr(a.t, cmd.Run())
}

var whitespaceAtStartOfLineRx = regexp.MustCompile(`(?m)^\s+`)

// AssertEqual compares the set of SQL statements to those in the given string
// literal. A test error is generated in case of differences. This assertion
// is lenient with regards to whitespace to enable callers to format their
// string literals in a way that fits nicely in the surrounding code.
func (a Assertable) AssertEqual(expected string) {
	a.t.Helper()
	// cleanup indentation and empty lines in `expected`
	expected = strings.TrimSpace(expected) + "\n"
	expected = whitespaceAtStartOfLineRx.ReplaceAllString(expected, "")

	// cleanup empty lines in `actual`
	actual := strings.ReplaceAll(a.payload, "\n\n", "\n")

	// quick path: if both are equal, we're fine
	if expected == actual {
		return
	}

	// slow path: show a diff
	tmpDir, err := os.MkdirTemp("", "easypg-diff")
	failOnErr(a.t, err)
	actualPath := filepath.Join(tmpDir, "/actual")
	failOnErr(a.t, os.WriteFile(actualPath, []byte(actual), 0o666))
	expectedPath := filepath.Join(tmpDir, "/expected")
	failOnErr(a.t, os.WriteFile(expectedPath, []byte(expected), 0o666))

	diffCmd := osext.GetenvOrDefault("GOBITS_DIFF_CMD", "diff")
	diffCmdSlice := []string{}
	if diffCmd == "diff" {
		diffCmdSlice = append(diffCmdSlice, "-u", "--color=always")
	}
	diffCmdSlice = append(diffCmdSlice, expectedPath, actualPath)
	cmd := exec.Command(diffCmd, diffCmdSlice...)
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	failOnErr(a.t, cmd.Run())
}

// AssertEqualf is a shorthand for AssertEqual(fmt.Sprintf(...)).
func (a Assertable) AssertEqualf(format string, args ...any) {
	a.t.Helper()
	a.AssertEqual(fmt.Sprintf(format, args...))
}

// AssertEmpty is a shorthand for AssertEqual("").
func (a Assertable) AssertEmpty() {
	a.t.Helper()
	a.AssertEqual("")
}

// Ignore is a no-op. It is commonly used like `tr.DBChanges().Ignore()`, to
// clarify that a certain set of DB changes is not asserted on.
func (a Assertable) Ignore() {
}

func failOnErr(t TestingT, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
