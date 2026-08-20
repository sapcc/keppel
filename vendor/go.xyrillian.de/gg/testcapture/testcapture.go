// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

// Package testcapture contains [Capture], a function that executes test code in a way that captures error messages and side effects without failing the overall test.
//
// The main intended use case is testing test assertions where calls to e.g. t.Error() are an expected part of a successful test run.
package testcapture

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
