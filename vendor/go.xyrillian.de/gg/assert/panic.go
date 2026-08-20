// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package assert

import (
	"go.xyrillian.de/gg/testcapture"
)

// Panics runs the provided action and fails the test if it does not panic.
// On success, the error value passed to the call of panic is returned.
func Panics(t TestingTB, action func()) any {
	return PanicsWith[any](t, action)
}

// PanicsWith is like Panics(), but also checks if the recovered error value
// is of type T, failing the test if the type assertion fails.
func PanicsWith[T any](t TestingTB, action func()) T {
	t.Helper()
	result := testcapture.Capture(t.Context(), t.Name(), func(_ TestingTB) {
		action()
	})
	if result.Outcome != testcapture.OutcomePanicked {
		t.Fatal("did not panic")
	}
	value, ok := result.Panic.(T)
	if !ok {
		var zero T
		t.Fatalf("panicked with incorrect type: expected %T, but got %T: %#v", zero, result.Panic, result.Panic)
	}
	return value
}
