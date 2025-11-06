// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package errext

import (
	"fmt"
	"os"
	"strings"

	"github.com/sapcc/go-bits/logg"
)

// ErrorSet replaces the "error" return value in functions that can return
// multiple errors. It provides convenience functions for easily adding errors
// to the set.
type ErrorSet []error

// Add adds the given error to the set if it is non-nil.
func (errs *ErrorSet) Add(err error) {
	if err != nil {
		*errs = append(*errs, err)
	}
}

// Addf is a shorthand for errs.Add(fmt.Errorf(...)).
func (errs *ErrorSet) Addf(msg string, args ...any) {
	*errs = append(*errs, fmt.Errorf(msg, args...))
}

// Append adds all errors from the `other` ErrorSet to this one.
func (errs *ErrorSet) Append(other ErrorSet) {
	*errs = append(*errs, other...)
}

// IsEmpty returns true if no errors are in the set.
func (errs ErrorSet) IsEmpty() bool {
	return len(errs) == 0
}

// Join joins the messages of all errors in this set using the provided separator.
// If the set is empty, an empty string is returned.
func (errs ErrorSet) Join(sep string) string {
	return errs.JoinedError(sep).Error()
}

// JoinedError is like Join, but returns an error that can be unwrapped.
func (errs ErrorSet) JoinedError(sep string) error {
	return joinedError{[]error(errs), sep}
}

// LogFatalIfError reports all errors in this set on level FATAL, thus dying if
// there are any errors.
func (errs ErrorSet) LogFatalIfError() {
	hasErrors := false
	for _, err := range errs {
		hasErrors = true
		logg.Other("FATAL", err.Error())
	}
	if hasErrors {
		os.Exit(1)
	}
}

type joinedError struct {
	errs      []error
	separator string
}

// Error implements the builtin/error interface.
func (e joinedError) Error() string {
	if len(e.errs) == 0 {
		return ""
	}
	msgs := make([]string, len(e.errs))
	for idx, err := range e.errs {
		msgs[idx] = err.Error()
	}
	return strings.Join(msgs, e.separator)
}

// Unwrap implements the interface implied by package errors.
func (e joinedError) Unwrap() []error {
	return e.errs
}
