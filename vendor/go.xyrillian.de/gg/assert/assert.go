// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

// Package assert contains assertions for use in unit tests.
// Each assertion in this package returns a bool to indicate whether the check succeeded, and logs a t.Error() when the check does not succeed.
package assert

import (
	"context"
	"io"
	"testing"
)

// TestingTB contains all the public functions of [testing.TB] (as of Go 1.26).
// Functions in this package use this type instead of [testing.TB] because the capture device used by package testcapture cannot implement [testing.TB]: It contains methods that are private to the standard library.
type TestingTB interface {
	ArtifactDir() string
	Attr(key, value string)
	Chdir(dir string)
	Cleanup(func())
	Context() context.Context
	Error(args ...any)
	Errorf(format string, args ...any)
	Fail()
	Failed() bool
	FailNow()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
	Helper()
	Log(args ...any)
	Logf(format string, args ...any)
	Name() string
	Output() io.Writer
	Setenv(key, value string)
	Skip(args ...any)
	Skipf(format string, args ...any)
	SkipNow()
	Skipped() bool
	TempDir() string
}

var _ TestingTB = testing.TB(nil)
