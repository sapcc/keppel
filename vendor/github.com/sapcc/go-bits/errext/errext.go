// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

// Package errext contains convenience functions for handling and propagating errors.
package errext

import "errors"

// As is a variant of errors.As() that leverages generics to present a nicer interface.
//
//	//this code:
//	var perr os.PathError
//	if errors.As(err, &perr) {
//		handle(perr)
//	}
//	//can be rewritten as:
//	if perr, ok := errext.As[os.PathError](err); ok {
//		handle(perr)
//	}
//
// This is sometimes more verbose (like in this example), but allows to scope
// the specific error variable to the condition's then-branch, and also looks
// more idiomatic to developers already familiar with type casts.
func As[T error](err error) (T, bool) {
	var result T
	ok := errors.As(err, &result)
	return result, ok
}

// IsOfType is a variant of errors.As() that only returns whether the match succeeded.
//
// This function is not called Is() to avoid confusion with errors.Is(), which works differently.
func IsOfType[T error](err error) bool {
	_, ok := As[T](err)
	return ok
}
