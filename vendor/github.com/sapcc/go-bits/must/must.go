// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

// Package must contains convenience functions for quickly exiting on fatal
// errors without the need for excessive "if err != nil".
package must

import (
	"testing"

	"github.com/sapcc/go-bits/logg"
)

// Succeed logs a fatal error and terminates the program if the given error is
// non-nil. For example, the following:
//
//	fileContents := []byte("loglevel = info")
//	err := os.WriteFile("config.ini", fileContents, 0666)
//	if err != nil {
//	  logg.Fatal(err.Error())
//	}
//
// can be shortened to:
//
//	fileContents := []byte("loglevel = info")
//	must.Succeed(os.WriteFile("config.ini", fileContents, 0666))
func Succeed(err error) {
	if err != nil {
		logg.Fatal(err.Error())
	}
}

// SucceedT is a variant of Succeed() for use in unit tests.
// Instead of exiting the program, any non-nil errors are reported with t.Fatal().
func SucceedT(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err.Error())
	}
}

// Return is like Succeed(), except that it propagates a result value on success.
// This can be chained with functions returning a pair of result value and error
// if errors are considered fatal. For example, the following:
//
//	buf, err := os.ReadFile("config.ini")
//	if err != nil {
//	  logg.Fatal(err.Error())
//	}
//
// can be shortened to:
//
//	buf := must.Return(os.ReadFile("config.ini"))
func Return[V any](val V, err error) V {
	Succeed(err)
	return val
}

// ReturnT is a variant of Return() for use in unit tests.
// Instead of exiting the program, any non-nil errors are reported with t.Fatal().
// For example:
//
//	buf := must.ReturnT(os.ReadFile("config.ini"))(t)
func ReturnT[V any](val V, err error) func(testing.TB) V {
	// NOTE: This is the only function signature that works. We cannot do something like
	//
	//	myMust := must.WithT(t)
	//	buf := myMust.Return(os.ReadFile("config.ini"))
	//
	// because then the type argument V would have to be introduced within a method of typeof(myMust),
	// but Go generics do not allow introducing new type arguments in methods. We also cannot do something like
	//
	//	buf := must.ReturnT(t, os.ReadFile("config.ini"))
	//
	// because filling multiple arguments using a call expression with multiple return values
	// is only allowed when there are no other arguments.
	return func(t testing.TB) V {
		t.Helper()
		SucceedT(t, err)
		return val
	}
}

// BeOK is like Return(), except that it uses a "ok" bool instead of an error.
// This can be chained with functions returning a pair of result value and bool
// if the bool being false is considered fatal. For example, the following:
//
//	val, ok := myMap["foo"]
//	if !ok {
//	  logg.Fatal("expected value to be present")
//	}
//
// can be shortened to:
//
//	val := must.BeOK(myMap["foo"])
func BeOK[V any](val V, ok bool) V {
	if !ok {
		logg.Fatal(`expected "ok" to be true`)
	}
	return val
}

// BeOKT is a variant of BeOK() for use in unit tests.
// Instead of exiting the program, any non-ok bool is reported with t.Fatal().
// For example:
//
//	val := must.BeOK(myMap["foo"])(t)
func BeOKT[V any](val V, ok bool) func(testing.TB) V {
	return func(t testing.TB) V {
		t.Helper()
		if !ok {
			t.Fatal(`expected "ok" to be true`)
		}
		return val
	}
}

// NotBeOK is the opposite operation of BeOK().
func NotBeOK[V any](val V, ok bool) V {
	if ok {
		logg.Fatal(`expected "ok" to be false`)
	}
	return val
}

// NotBeOKT is the opposite operation of BeOKT().
func NotBeOKT[V any](val V, ok bool) func(testing.TB) V {
	return func(t testing.TB) V {
		t.Helper()
		if ok {
			t.Fatal(`expected "ok" to be false`)
		}
		return val
	}
}
