// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

// Package must contains convenience functions for quickly exiting on fatal
// errors without the need for excessive "if err != nil".
package must

import "github.com/sapcc/go-bits/logg"

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
func Return[T any](val T, err error) T {
	Succeed(err)
	return val
}
