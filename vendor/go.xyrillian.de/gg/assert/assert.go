// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

// Package assert contains assertions for use in unit tests.
// Each assertion in this package returns a bool to indicate whether the check succeeded, and logs a t.Error() when the check does not succeed.
package assert

import "go.xyrillian.de/gg/testcapture"

// TestingTB contains all the public functions of [testing.TB] (as of Go 1.26).
// Functions in this package use this type instead of [testing.TB] to allow
// mocks of [testing.TB] to be substituted in tests for this package.
type TestingTB = testcapture.TestingTB
