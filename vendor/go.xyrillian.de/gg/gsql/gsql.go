// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

// Package gsql abstracts over database libraries, supporting both database/sql drivers and non-standard drivers like [pgx].
// The main abstractions are [Handle] and [ConnectionHandle].
//
// This package only provides [Handle] implementations for use with database/sql.
// A [Handle] implementation for use with [pgx] is provided in [gg-pgx].
//
// [pgx]: https://github.com/jackc/pgx
// [gg-pgx]: https://git.xyrillian.de/go-gg-pgx/
package gsql

import (
	"context"
	"database/sql"
)

// ConnectionHandle extends [Handle] with methods that make sense for handles referring to entire connections or connection pools, but not e.g. to transactions.
// The standard-library types [*sql.DB] and [*sql.Conn] can satisfy this interface through the wrappers [NewDB] and [NewConn].
//
// Like for [Handle], the method names are deliberately clunky to avoid name clashes with well-known methods.
type ConnectionHandle interface {
	Handle

	// GSQLClose closes the connection represented by this handle.
	// Once this method has been called, other methods must not be called.
	GSQLClose(ctx context.Context) error

	// GSQLTransact executes an action within a database transaction.
	// The callback will be provided with a [Handle] referring to the transaction.
	// The transaction will be committed if the callback returns successfully, or rolled back otherwise.
	GSQLTransact(ctx context.Context, action func(tx Handle) error) error
}

// Handle can be implemented by objects that allow executing SQL queries.
// The standard-library types [*sql.DB], [*sql.Conn] and [*sql.Tx] can satisfy this interface through the wrappers [NewDB], [NewConn] and [NewTx].
// Custom implementations of this interface can be used to connect non-std database drivers to functions accepting this interface.
//
// The method names are deliberately clunky to avoid name clashes with well-known methods like [sql.DB.Prepare] or [sql.DB.Query].
type Handle interface {
	// GSQLPrepare prepares to execute a certain SQL query one or multiple times.
	//
	// The "repeated" flag is a hint to the implementation whether the same statement is going to be run many times.
	// If false, the implementation shall choose to forego the additional effort of a full statement preparation if possible,
	// and execute one-off queries instead.
	GSQLPrepare(ctx context.Context, query string, repeated bool) (Statement, error)

	// GSQLQuery works like db.QueryContext(ctx, query, args...).
	GSQLQuery(ctx context.Context, query string, args []any) (Rows, error)
}

// Statement represents a prepared statement returned from the GSQLPrepare() method of [Handle].
// The Exec and QueryRow methods shall work similarly to the respective functions on [*sql.Tx], as indicated in the comments.
//
// You will not need to interact with this type except when implementing your own [Handle].
type Statement interface {
	Close() error

	// Exec works like stmt.ExecContext(ctx, args...).
	// The returned Result object must remain usable after Close() is called on this Statement instance.
	Exec(ctx context.Context, args []any) (sql.Result, error)

	// QueryRow works like stmt.QueryRow(ctx, args...).Scan(slots...).
	QueryRow(ctx context.Context, args []any, slots []any) error
}

// Rows represents a set of rows returned from the GSQLQuery() method of [Handle].
// All methods shall behave like on the [*sql.Rows] type from std.
//
// You will not need to interact with this type except when implementing your own [Handle].
type Rows interface {
	Columns() ([]string, error)
	Close() error
	Err() error
	Next() bool
	Scan(slots ...any) error
}
