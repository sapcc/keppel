// SPDX-FileCopyrightText: 2017 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package assert

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/sapcc/go-bits/osext"
)

// Equal checks if the actual and expected value are equal according to == rules, and t.Errors() otherwise.
func Equal[V comparable](t TestingT, actual, expected V) bool {
	t.Helper()
	if actual == expected {
		return true
	}
	t.Errorf("expected %#v, but got %#v", expected, actual)
	return false
}

// ErrEqual checks if the actual error matches the expectation.
//
//   - If `expected` is nil, the actual error must be nil.
//   - If `expected` is of type error, the actual error must be exactly equal to it, or contain it in the sense of errors.Is().
//   - If `expected` is of type string, the actual error message must be exactly equal to it.
//   - If `expected` is of type *regexp.Regexp, that regexp must match the actual error message.
func ErrEqual(t TestingT, actual error, expectedErrorOrMessageOrRegexp any) bool {
	// NOTE 1: We cannot enumerate the possible types of `expected` as a type argument of the form
	//             func ErrEqual[T interface{ error | string | *regexp.Regexp }](...)
	//         because unions of interface types (error) and concrete types (string etc.) are not permitted.
	//         The risk of accepting an `any` value is acceptable here because the panic from
	//         using an unexpected type can only occur in tests, and thus will be difficult to overlook.
	//
	// NOTE 2: The verbose name of the last argument is intended to help users
	//         who see only the function signature in their IDE autocomplete.
	t.Helper()

	switch expected := expectedErrorOrMessageOrRegexp.(type) {
	case nil:
		if actual == nil {
			return true
		}
		t.Errorf("expected success, but got error: %s", actual.Error())
		return false

	case error:
		if actual == nil {
			if expected == nil {
				// defense in depth: this should have been covered by the previous case branch
				return true
			}
			t.Errorf("expected error stack to contain %q, but got no error", expected.Error())
			return false
		}
		switch {
		case expected == nil:
			// defense in depth: this should have been covered by the previous case branch
			t.Errorf("expected success, but got error: %s", actual.Error())
			return false
		case errors.Is(actual, expected):
			return true
		default:
			t.Errorf("expected error stack to contain %q, but got error: %s", expected.Error(), actual.Error())
			return false
		}

	case string:
		if actual == nil {
			if expected == "" {
				return true
			}
			t.Errorf("expected error with message %q, but got no error", expected)
			return false
		}
		msg := actual.Error()
		switch expected {
		case "":
			t.Errorf("expected success, but got error: %s", msg)
			return false
		case msg:
			return true
		default:
			t.Errorf("expected error with message %q, but got error: %s", expected, msg)
			return false
		}

	case *regexp.Regexp:
		if actual == nil {
			t.Errorf("expected error with message matching /%s/, but got no error", expected.String())
			return false
		}
		msg := actual.Error()
		if expected.MatchString(msg) {
			return true
		}
		t.Errorf("expected error with message matching /%s/, but got error: %s", expected.String(), msg)
		return false

	default:
		panic(fmt.Sprintf("assert.ErrEqual() cannot match against an expectation of type %T", expected))
	}
}

// DeepEqual checks if the actual and expected value are equal as
// determined by reflect.DeepEqual(), and t.Error()s otherwise.
func DeepEqual[V any](t *testing.T, variable string, actual, expected V) bool {
	t.Helper()
	if reflect.DeepEqual(actual, expected) {
		return true
	}

	//NOTE: We HAVE TO use %#v here, even if it's verbose. Every other generic
	// formatting directive will not correctly distinguish all values, and thus
	// possibly render empty diffs on failure. For example,
	//
	//	fmt.Sprintf("%+v\n", []string{})    == "[]\n"
	//	fmt.Sprintf("%+v\n", []string(nil)) == "[]\n"
	//
	t.Error("assert.DeepEqual failed for " + variable)
	if osext.GetenvBool("GOBITS_PRETTY_DIFF") {
		dmp := diffmatchpatch.New()
		diffs := dmp.DiffMain(fmt.Sprintf("%#v\n", actual), fmt.Sprintf("%#v\n", expected), false)
		t.Log(dmp.DiffPrettyText(diffs))
	} else {
		t.Logf("\texpected = %#v\n", expected)
		t.Logf("\t  actual = %#v\n", actual)
	}

	return false
}

// TestingT is an interface implemented by the *testing.T type.
// Some tests inside go-bits use this interface to substitute a mock for the real *testing.T type.
type TestingT interface {
	Helper()
	Errorf(msg string, args ...any)
}
