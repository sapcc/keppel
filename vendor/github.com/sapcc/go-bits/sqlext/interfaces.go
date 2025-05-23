// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package sqlext

import "database/sql"

// Executor contains the common methods that both SQL connections (*sql.DB) and
// transactions (*sql.Tx) implement. This is useful for functions that don't
// care whether they execute within a transaction or not.
//
// For compatibility with applications using gorp (the ORM library), this
// interface only contains methods that are also implemented by *gorp.DbMap and
// *gorp.Transaction. This interface is therefore a restricted version of type
// gorp.SqlExecutor, from which it inherits its name.
type Executor interface {
	Exec(query string, args ...any) (sql.Result, error)
	Prepare(query string) (*sql.Stmt, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// Rollbacker contains the Rollback() method from *sql.Tx. This interface is
// also satisfied by other types with transaction-like behavior like
// *gorp.Transaction.
type Rollbacker interface {
	Rollback() error
}

// verify interface coverage
var _ Executor = &sql.DB{}
var _ Executor = &sql.Tx{}
var _ Rollbacker = &sql.Tx{}
