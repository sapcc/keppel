// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package errext

import "fmt"

// WithCleanup combines two errors into a single error:
//   - (optionally) a main error from an IO operation (e.g. a directory listing or a database read)
//   - an auxiliary error from closing or otherwise cleaning up the respective IO handle
//
// It is intended to simplify situations where a Close() or Commit()/Rollback() call is required at the end of the operation,
// but its error should not be swallowed if there is one.
// For example:
//
//	db, err := sql.Open("postgres", dbURL)
//	if err != nil {
//		return nil, err
//	}
//	err = applySchema(db)
//	if err != nil {
//		err2 := db.Close()
//		if err2 == nil {
//			return nil, err
//		} else {
//			return nil, fmt.Errorf("%w (additional error during db.Close(): %s)", err, err2.Error())
//		}
//	}
//	return db, nil
//
// can be shortened to:
//
//	db, err := sql.Open("postgres", dbURL)
//	if err != nil {
//		return nil, err
//	}
//	err = applySchema(db)
//	if err != nil {
//		return errext.WithCleanup(err, "db.Close", db.Close())
//	}
//	return db, nil
//
// If cleanupErr is nil, err is returned unchanged.
//
// If err is nil, but cleanupErr is not nil, cleanupErr will be wrapped to indicate the name of the cleanup operation that failed.
func WithCleanup(err error, cleanupOperation string, cleanupErr error) error {
	if cleanupErr == nil {
		return err
	}
	return cleanupError{err, cleanupErr, cleanupOperation}
}

type cleanupError struct {
	MainError        error
	CleanupError     error
	CleanupOperation string
}

// Error implements the builtin/error interface.
func (e cleanupError) Error() string {
	if e.MainError == nil {
		return fmt.Sprintf("during %s(): %s", e.CleanupOperation, e.CleanupError.Error())
	} else {
		return fmt.Sprintf("%s (additional error during %s(): %s)", e.MainError.Error(), e.CleanupOperation, e.CleanupError.Error())
	}
}

// Unwrap implements the interface implied by the documentation of package errors.
func (e cleanupError) Unwrap() []error {
	if e.MainError == nil {
		return []error{e.CleanupError}
	} else {
		return []error{e.MainError, e.CleanupError}
	}
}
