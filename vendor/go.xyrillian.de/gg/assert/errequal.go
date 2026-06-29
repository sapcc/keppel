// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package assert

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
)

// ErrEqual checks if the actual error matches the expectation.
//   - If expected is nil, the actual error must be nil.
//   - If expected is of type error, the actual error must be exactly equal to it or contain it, as reported by the [errors.Is] function.
//   - If expected is of type string, the actual error must have a message exactly equal to it.
//   - If expected is of type [*regexp.Regexp], the actual error must have a message matching that regexp.
//   - If expected is of any other type, ErrEqual will panic.
func ErrEqual(t TestingTB, actual error, expected any) bool {
	t.Helper()
	err := errEqual(t, actual, expected)
	if err == nil {
		return true
	} else {
		t.Error(err)
		return false
	}
}

// ErrsEqual checks if a list of actual errors matches the expectation.
// Both lists must be of equal length, and each individual entry must match according to [ErrEqual].
func ErrsEqual[A ~[]error, E ~[]V, V any](t TestingTB, actual A, expected E) bool {
	t.Helper()
	ok := true
	for idx := range max(len(actual), len(expected)) {
		var err error
		switch {
		case idx >= len(actual):
			err = errEqual(t, missingError{}, expected[idx])
		case idx >= len(expected):
			err = errEqual(t, actual[idx], missingError{})
		default:
			err = errEqual(t, actual[idx], expected[idx])
		}
		if err != nil {
			t.Errorf("in actual[%d]: %s", idx, err)
			ok = false
		}
	}
	return ok
}

// missingError is used by ErrsEqual() to mark a missing error on one side of the match.
type missingError struct{}

func (missingError) Error() string {
	return "<missing>"
}

func errEqual(t TestingTB, actual error, expected any) error {
	// coerce all types that implement `error` into the interface type `error`,
	// and also coerce all nil values of concrete `error` types into untyped nil
	if expectedErr, ok := expected.(error); ok {
		// convert nil values of concrete error types into a generic nil value
		expectedValue := reflect.ValueOf(expectedErr)
		kind := expectedValue.Kind()
		if (kind == reflect.Pointer || kind == reflect.Interface) && expectedValue.IsNil() {
			expected = nil
		} else {
			expected = expectedErr
		}
	}

	switch expected := expected.(type) {
	case nil:
		if actual == nil {
			return nil
		} else {
			return fmt.Errorf("expected no error, but got %s", formatErrorMessage(actual))
		}
	case error:
		if actual == nil {
			return fmt.Errorf("expected %s, but got no error", formatErrorMessage(expected))
		} else if errors.Is(actual, expected) {
			return nil
		} else {
			return fmt.Errorf("expected %s, but got %q", formatErrorMessage(expected), actual.Error())
		}
	case string:
		if actual == nil {
			return fmt.Errorf("expected %q, but got no error", expected)
		} else if actual.Error() == expected {
			return nil
		} else {
			return fmt.Errorf("expected %q, but got %s", expected, formatErrorMessage(actual))
		}
	case *regexp.Regexp:
		if actual == nil {
			return fmt.Errorf("expected an error matching /%s/, but got no error", expected.String())
		} else if expected.MatchString(actual.Error()) {
			return nil
		} else {
			return fmt.Errorf("expected an error matching /%s/, but got %s", expected.String(), formatErrorMessage(actual))
		}
	default:
		panic(fmt.Sprintf("cannot handle `expected` of type %T", expected))
	}
}

func formatErrorMessage(err error) string {
	if _, ok := err.(missingError); ok { //nolint:errorlint // this error is never wrapped, so errors.Is() is unnecessary
		return err.Error()
	} else {
		return fmt.Sprintf("%q", err.Error())
	}
}
