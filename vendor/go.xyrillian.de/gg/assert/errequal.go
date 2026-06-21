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
			return true
		} else {
			t.Errorf("expected no error, but got %q", actual.Error())
			return false
		}
	case error:
		if actual == nil {
			t.Errorf("expected %q, but got no error", expected.Error())
			return false
		} else if errors.Is(actual, expected) {
			return true
		} else {
			t.Errorf("expected %q, but got %q", expected.Error(), actual.Error())
			return false
		}
	case string:
		if actual == nil {
			t.Errorf("expected %q, but got no error", expected)
			return false
		} else if actual.Error() == expected {
			return true
		} else {
			t.Errorf("expected %q, but got %q", expected, actual.Error())
			return false
		}
	case *regexp.Regexp:
		if actual == nil {
			t.Errorf("expected an error matching /%s/, but got no error", expected.String())
			return false
		} else if expected.MatchString(actual.Error()) {
			return true
		} else {
			t.Errorf("expected an error matching /%s/, but got %q", expected.String(), actual.Error())
			return false
		}
	default:
		panic(fmt.Sprintf("cannot handle `expected` of type %T", expected))
	}
}
