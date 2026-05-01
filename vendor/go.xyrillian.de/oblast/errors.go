// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package oblast

import (
	"fmt"
	"reflect"
	"strings"
)

// MissingRecordError is returned by [Store.Update] if one of the rows to be updated does not exist in the DB.
type MissingRecordError[R any] struct {
	// The record that was provided to [Store.Update],
	// but for which no row with the same primary key values could be located.
	Record R
	plan   plan
}

// Error implements the builtin/error interface.
func (e MissingRecordError[R]) Error() string {
	keyDescs := make([]string, len(e.plan.PrimaryKeyColumnNames))
	v := reflect.ValueOf(e.Record)
	for idx, columnName := range e.plan.PrimaryKeyColumnNames {
		keyDescs[idx] = fmt.Sprintf("%s = %#v", columnName, v.FieldByIndex(e.plan.IndexByColumnName[columnName]))
	}
	return "could not UPDATE record that does not exist in the database: " + strings.Join(keyDescs, ", ")
}

// ioError is an error type that contains:
// - (optionally) a main error from an IO operation (e.g. a database read)
// - an auxiliary error from closing or otherwise cleaning up the respective IO handle
//
// This is only used when there is a cleanup error.
// Otherwise, the main error will be returned without being wrapped in this type.
type ioError struct {
	MainError        error
	CleanupError     error
	CleanupOperation string
}

func newIOError(err error, cleanupOperation string, cleanupErr error) error {
	if cleanupErr == nil {
		return err
	}
	return ioError{err, cleanupErr, cleanupOperation}
}

// Error implements the builtin/error interface.
func (e ioError) Error() string {
	if e.MainError == nil {
		return fmt.Sprintf("during %s(): %s", e.CleanupOperation, e.CleanupError.Error())
	} else {
		return fmt.Sprintf("%s (additional error during %s(): %s)", e.MainError.Error(), e.CleanupOperation, e.CleanupError.Error())
	}
}

// Unwrap implements the interface implied by the documentation of package errors.
func (e ioError) Unwrap() []error {
	if e.MainError == nil {
		return []error{e.CleanupError}
	} else {
		return []error{e.MainError, e.CleanupError}
	}
}
